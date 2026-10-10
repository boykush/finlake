// finlake は家計データのパイプラインと、それを配る MCP サーバーをまとめた CLI。
// 1つのイメージをサブコマンドで使い分ける。
//
//	finlake ingest    [--month YYYY-MM|current|previous]  マネーフォワード ME の CSV を raw 層へ
//	finlake backfill  --since YYYY-MM [--until ...]       過去月の CSV をまとめて取得し、明細にする
//	finlake transform [--month YYYY-MM|current|previous]  raw 層の CSV を product の明細へ
//	finlake sync      [--dry-run]                         product より新しい raw 層の CSV を明細へ
//	finlake mcp       [--addr host:port]                  明細を MCP で配る
//	finlake duckdb-extensions <dir>                       DuckDB の拡張を dir に入れる（イメージのビルド用）
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/boykush/finlake/internal/ingest"
	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/mcpserver"
	"github.com/boykush/finlake/internal/moneyforward"
	"github.com/boykush/finlake/internal/month"
	"github.com/boykush/finlake/internal/transform"
)

// version はビルド時に ldflags で埋める。
var version = "dev"

const usage = `usage: finlake <command> [flags]

commands:
  ingest              download the Money Forward ME CSV of a month into the raw layer
  backfill            download the Money Forward ME CSVs of past months and transform them
  transform           convert the raw CSV of a month into the product transactions
  sync                transform every month whose raw CSV is newer than its product
  mcp                 serve the product transactions over MCP
  duckdb-extensions   install DuckDB extensions into a directory (image build)
  version             print the version
`

// lakeEnv はデータレイクへの接続設定。どのサブコマンドも読む。
type lakeEnv struct {
	Root               string `env:"FINLAKE_LAKE_ROOT,notEmpty"`
	S3Endpoint         string `env:"FINLAKE_S3_ENDPOINT"`
	S3Region           string `env:"FINLAKE_S3_REGION"`
	S3URLStyle         string `env:"FINLAKE_S3_URL_STYLE"`
	S3AccessKeyID      string `env:"FINLAKE_S3_ACCESS_KEY_ID"`
	S3SecretAccessKey  string `env:"FINLAKE_S3_SECRET_ACCESS_KEY"`
	ExtensionDirectory string `env:"FINLAKE_DUCKDB_EXTENSION_DIRECTORY"`
}

// moneyforwardEnv はマネーフォワード ME から取得するサブコマンドが読む。
type moneyforwardEnv struct {
	Cookie string `env:"MONEYFORWARD_COOKIE,notEmpty"`
}

// mcpEnv は mcp が読む。家計は非公開の情報なので、トークンが無ければ起動しない。
type mcpEnv struct {
	Tokens string `env:"FINLAKE_MCP_TOKENS,notEmpty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("finlake failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command")
	}
	cmd, args := args[0], args[1:]

	switch cmd {
	case "ingest":
		return runIngest(ctx, args)
	case "backfill":
		return runBackfill(ctx, args)
	case "transform":
		return runTransform(ctx, args)
	case "sync":
		return runSync(ctx, args)
	case "mcp":
		return runMCP(ctx, args)
	case "duckdb-extensions":
		if len(args) != 1 {
			return errors.New("usage: finlake duckdb-extensions <dir>")
		}
		return lake.InstallExtensions(ctx, args[0])
	case "version":
		fmt.Println(version)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func monthFlag(name string, args []string) (month.Month, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	m := fs.String("month", "current", "target month: YYYY-MM, current or previous (in JST)")
	if err := fs.Parse(args); err != nil {
		return month.Month{}, err
	}
	return month.Parse(*m, time.Now())
}

func runIngest(ctx context.Context, args []string) error {
	m, err := monthFlag("ingest", args)
	if err != nil {
		return err
	}
	mf, err := env.ParseAs[moneyforwardEnv]()
	if err != nil {
		return err
	}
	l, err := openLake(ctx)
	if err != nil {
		return err
	}
	defer l.Close()

	dst, err := ingest.Run(ctx, l, moneyforward.NewClient(mf.Cookie), m)
	if err != nil {
		return err
	}
	// path は設定（FINLAKE_LAKE_ROOT）から作ったもので、外部入力ではない。
	slog.Info("ingested", "month", m.String(), "path", dst) //nolint:gosec // G706: see above
	return nil
}

// backfillInterval は月ごとのダウンロードの間隔。マネーフォワードに続けざまに当てない。
var backfillInterval = time.Second

func runBackfill(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	since := fs.String("since", "", "first month: YYYY-MM (required)")
	until := fs.String("until", "previous", "last month: YYYY-MM, current or previous (in JST)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *since == "" {
		return errors.New("backfill: --since is required")
	}
	now := time.Now()
	from, err := month.Parse(*since, now)
	if err != nil {
		return err
	}
	to, err := month.Parse(*until, now)
	if err != nil {
		return err
	}
	if to.Compare(from) < 0 {
		return fmt.Errorf("backfill: --until %s is before --since %s", to, from)
	}
	mf, err := env.ParseAs[moneyforwardEnv]()
	if err != nil {
		return err
	}

	l, err := openLake(ctx)
	if err != nil {
		return err
	}
	defer l.Close()

	return backfill(ctx, l, moneyforward.NewClient(mf.Cookie), from, to)
}

// backfill は from から to までの月を古い順に取得して取り込む。明細の無い月は飛ばす。
func backfill(ctx context.Context, l *lake.Lake, client *moneyforward.Client, from, to month.Month) error {
	for m := from; m.Compare(to) <= 0; m = m.Add(1) {
		if m != from {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backfillInterval):
			}
		}
		body, err := client.DownloadCSV(ctx, m)
		if err != nil {
			return fmt.Errorf("backfill %s: %w", m, err)
		}
		months, err := moneyforward.Months(body)
		if err != nil {
			return fmt.Errorf("backfill %s: %w", m, err)
		}
		if len(months) == 0 {
			slog.Info("no transactions, skip", "month", m.String())
			continue
		}
		if _, err := ingest.Write(ctx, l, m, body); err != nil {
			return fmt.Errorf("backfill %s: %w", m, err)
		}
		n, dst, err := transform.Run(ctx, l, m)
		if err != nil {
			return err
		}
		// path は設定（FINLAKE_LAKE_ROOT）から作ったもので、外部入力ではない。
		slog.Info("backfilled", "month", m.String(), "rows", n, "path", dst) //nolint:gosec // G706: see above
	}
	return nil
}

func runTransform(ctx context.Context, args []string) error {
	m, err := monthFlag("transform", args)
	if err != nil {
		return err
	}
	l, err := openLake(ctx)
	if err != nil {
		return err
	}
	defer l.Close()

	n, dst, err := transform.Run(ctx, l, m)
	if err != nil {
		return err
	}
	// path は設定（FINLAKE_LAKE_ROOT）から作ったもので、外部入力ではない。
	slog.Info("transformed", "month", m.String(), "rows", n, "path", dst) //nolint:gosec // G706: see above
	return nil
}

func runSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "list the months to transform without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	l, err := openLake(ctx)
	if err != nil {
		return err
	}
	defer l.Close()

	return syncPending(ctx, l, *dryRun)
}

// syncPending は変換が要る月をすべて変換する。1つの月が失敗しても残りは続け、最後にまとめて返す。
func syncPending(ctx context.Context, l *lake.Lake, dryRun bool) error {
	months, err := transform.Pending(ctx, l)
	errs := []error{err}
	if len(months) == 0 {
		slog.Info("nothing to transform")
	}
	for _, m := range months {
		if dryRun {
			slog.Info("pending", "month", m.String())
			continue
		}
		n, dst, err := transform.Run(ctx, l, m)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// path は設定（FINLAKE_LAKE_ROOT）から作ったもので、外部入力ではない。
		slog.Info("transformed", "month", m.String(), "rows", n, "path", dst) //nolint:gosec // G706: see above
	}
	return errors.Join(errs...)
}

func runMCP(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	addr := fs.String("addr", "0.0.0.0:8080", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	e, err := env.ParseAs[mcpEnv]()
	if err != nil {
		return err
	}
	tokens := splitList(e.Tokens)
	if len(tokens) == 0 {
		return errors.New("FINLAKE_MCP_TOKENS is empty: the MCP server never runs without authentication")
	}

	l, err := openLake(ctx)
	if err != nil {
		return err
	}
	defer l.Close()

	server := mcpserver.NewServer(mcpserver.NewStore(l), version)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           mcpserver.Handler(server, tokens),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("serving MCP", "addr", *addr, "version", version)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func openLake(ctx context.Context) (*lake.Lake, error) {
	e, err := env.ParseAs[lakeEnv]()
	if err != nil {
		return nil, err
	}
	return lake.Open(ctx, lake.Config{
		Root: e.Root,
		S3: lake.S3Config{
			Endpoint:        e.S3Endpoint,
			Region:          e.S3Region,
			URLStyle:        e.S3URLStyle,
			AccessKeyID:     e.S3AccessKeyID,
			SecretAccessKey: e.S3SecretAccessKey,
		},
		ExtensionDirectory: e.ExtensionDirectory,
	})
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
