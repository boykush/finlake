// Package transform は raw 層のマネーフォワード CSV を product の明細（Parquet）に変換する。
package transform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/moneyforward"
	"github.com/boykush/finlake/internal/month"
)

// transactionsSQL は raw の CSV を明細にする SELECT。{{raw}} を読み込み元に置き換えて使う。
//
//go:embed transactions.sql
var transactionsSQL string

// Run は対象月の raw CSV を読み、明細を Parquet で書き出す。書き出した件数を返す。
func Run(ctx context.Context, l *lake.Lake, m month.Month) (int64, string, error) {
	src, cleanup, err := stage(ctx, l, m)
	if err != nil {
		return 0, "", fmt.Errorf("transform %s: %w", m, err)
	}
	defer cleanup()

	dst := l.TransactionsParquet(m)
	if err := l.PrepareWrite(dst); err != nil {
		return 0, "", err
	}

	sel := strings.ReplaceAll(transactionsSQL, "{{raw}}", lake.Quote(src))
	q := fmt.Sprintf("COPY (%s) TO %s (FORMAT parquet, COMPRESSION zstd)", sel, lake.Quote(dst))

	res, err := l.DB.ExecContext(ctx, q)
	if err != nil {
		return 0, "", fmt.Errorf("transform %s: %w", m, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, "", err
	}
	return n, dst, nil
}

// stage は対象月の raw CSV を確かめ、UTF-8 にして手元に置く。raw 層は人が手で置くこともあるので、
// 置き間違い（月に複数のファイル、別の月の明細）は product にせずここで弾く。
func stage(ctx context.Context, l *lake.Lake, m month.Month) (string, func(), error) {
	files, err := l.Modified(ctx, l.RawGlob(m))
	if err != nil {
		return "", nil, err
	}
	if len(files) != 1 {
		return "", nil, fmt.Errorf("want exactly one csv in %s, got %d", l.RawGlob(m), len(files))
	}
	var body []byte
	for name := range files {
		raw, err := l.ReadFile(ctx, name)
		if err != nil {
			return "", nil, err
		}
		if body, err = moneyforward.Normalize(raw); err != nil {
			return "", nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	months, err := moneyforward.Months(body)
	if err != nil {
		return "", nil, err
	}
	if !slices.Equal(months, []month.Month{m}) {
		return "", nil, fmt.Errorf("want only transactions of %s, got %v", m, months)
	}

	dir, err := os.MkdirTemp("", "finlake-transform-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	tmp := filepath.Join(dir, "transactions.csv")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp, cleanup, nil
}

// Pending は product が無いか、product より後に raw が置かれた月を古い順に返す。
// month=YYYY-MM でないフォルダはエラーに載せ、読めた月は返す。
func Pending(ctx context.Context, l *lake.Lake) ([]month.Month, error) {
	raws, err := l.Modified(ctx, l.RawGlobAll())
	if err != nil {
		return nil, err
	}
	products, err := l.Modified(ctx, l.TransactionsGlob())
	if err != nil {
		return nil, err
	}
	done := map[month.Month]time.Time{}
	for name, t := range products {
		if m, err := lake.MonthOf(name); err == nil {
			done[m] = t
		}
	}

	var pending []month.Month
	var errs []error
	for name, t := range raws {
		m, err := lake.MonthOf(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if p, ok := done[m]; (!ok || t.After(p)) && !slices.Contains(pending, m) {
			pending = append(pending, m)
		}
	}
	slices.SortFunc(pending, month.Month.Compare)
	return pending, errors.Join(errs...)
}
