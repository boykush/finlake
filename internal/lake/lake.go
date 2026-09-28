// Package lake は DuckDB で読み書きするデータレイクを表す。
//
// ルートは S3 互換のオブジェクトストレージ（s3://bucket/prefix。本番は Cloudflare R2）か
// ローカルのディレクトリで、
// どちらでも同じパス配置で読み書きする。
//
//	raw/moneyforward/month=YYYY-MM/transactions.csv    ingest が置くマネーフォワードの CSV
//	product/transactions/month=YYYY-MM/data.parquet    transform が作る明細
package lake

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/boykush/finlake/internal/month"
)

// Config はデータレイクへの接続設定。
type Config struct {
	// Root は s3://bucket[/prefix] かローカルのディレクトリ。
	Root string
	// S3 は Root が s3:// のときに使う。
	S3 S3Config
	// ExtensionDirectory は DuckDB の拡張（httpfs）を置くディレクトリ。
	// イメージはビルド時に入れた拡張をここから読む。
	ExtensionDirectory string
}

// S3Config は S3 互換ストレージ（Cloudflare R2 など）の設定。
type S3Config struct {
	Endpoint string
	Region   string
	// URLStyle は vhost（既定）か path。R2 は path で指す。
	URLStyle        string
	AccessKeyID     string
	SecretAccessKey string
}

// Lake は DuckDB の接続とデータレイクのルート。
type Lake struct {
	DB   *sql.DB
	root string
}

// Open は in-memory の DuckDB を開き、Root が s3:// なら httpfs と secret を用意する。
func Open(ctx context.Context, cfg Config) (*Lake, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("lake root is empty")
	}

	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db := sql.OpenDB(connector)
	// in-memory DB は接続ごとに別物になるので、secret と設定を共有するため1本に絞る。
	db.SetMaxOpenConns(1)

	l := &Lake{DB: db, root: strings.TrimSuffix(cfg.Root, "/")}
	if err := l.setup(ctx, cfg); err != nil {
		_ = db.Close()
		return nil, err
	}
	return l, nil
}

func (l *Lake) setup(ctx context.Context, cfg Config) error {
	var stmts []string
	if cfg.ExtensionDirectory != "" {
		stmts = append(stmts, fmt.Sprintf("SET extension_directory = %s", Quote(cfg.ExtensionDirectory)))
	}
	if l.IsRemote() {
		s3 := cfg.S3
		urlStyle := s3.URLStyle
		if urlStyle == "" {
			urlStyle = "vhost"
		}
		opts := []string{"TYPE s3", "URL_STYLE " + Quote(urlStyle)}
		for _, kv := range [][2]string{
			{"KEY_ID", s3.AccessKeyID},
			{"SECRET", s3.SecretAccessKey},
			{"REGION", s3.Region},
			{"ENDPOINT", s3.Endpoint},
		} {
			if kv[1] != "" {
				opts = append(opts, kv[0]+" "+Quote(kv[1]))
			}
		}
		// イメージは拡張をビルド時に焼いて ExtensionDirectory で渡すので、実行時は読むだけにする
		// （root filesystem が読み取り専用でも動くように）。手元では取りに行かせる。
		if cfg.ExtensionDirectory == "" {
			stmts = append(stmts, "INSTALL httpfs")
		}
		stmts = append(stmts,
			"LOAD httpfs",
			"CREATE OR REPLACE SECRET lake ("+strings.Join(opts, ", ")+")",
		)
	}
	for _, s := range stmts {
		if _, err := l.DB.ExecContext(ctx, s); err != nil {
			// secret の中身がエラーに載らないよう、文そのものは出さない。
			return fmt.Errorf("setup duckdb: %s: %w", strings.Fields(s)[0], err)
		}
	}
	return nil
}

// InstallExtensions は dir に httpfs を入れる。イメージのビルド時に呼び、
// 実行時にネットワークから拡張を取りに行かずに済むようにする。
func InstallExtensions(ctx context.Context, dir string) error {
	l, err := Open(ctx, Config{Root: dir, ExtensionDirectory: dir})
	if err != nil {
		return err
	}
	defer l.Close()
	_, err = l.DB.ExecContext(ctx, "INSTALL httpfs")
	return err
}

// Close は DuckDB を閉じる。
func (l *Lake) Close() error { return l.DB.Close() }

// IsRemote は Root が s3:// かどうか。
func (l *Lake) IsRemote() bool { return strings.HasPrefix(l.root, "s3://") }

// Path は Root からの相対パスを DuckDB に渡せるパスにする。
func (l *Lake) Path(elem ...string) string {
	if l.IsRemote() {
		return l.root + "/" + path.Join(elem...)
	}
	return filepath.Join(append([]string{l.root}, elem...)...)
}

// PrepareWrite はローカルのとき書き込み先のディレクトリを作る。DuckDB の COPY は親を作らない。
func (l *Lake) PrepareWrite(p string) error {
	if l.IsRemote() {
		return nil
	}
	return os.MkdirAll(filepath.Dir(p), 0o750)
}

// RawCSV はマネーフォワードの CSV を置くパス。
func (l *Lake) RawCSV(m month.Month) string {
	return l.Path("raw", "moneyforward", "month="+m.String(), "transactions.csv")
}

// TransactionsParquet は明細の product を置くパス。
func (l *Lake) TransactionsParquet(m month.Month) string {
	return l.Path("product", "transactions", "month="+m.String(), "data.parquet")
}

// TransactionsGlob はすべての月の明細に当たる glob。
func (l *Lake) TransactionsGlob() string {
	return l.Path("product", "transactions", "*", "data.parquet")
}

// Quote は SQL の文字列リテラルにする。COPY の宛先や secret はパラメータで渡せないため。
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
