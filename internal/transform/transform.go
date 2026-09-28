// Package transform は raw 層のマネーフォワード CSV を product の明細（Parquet）に変換する。
package transform

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/month"
)

// transactionsSQL は raw の CSV を明細にする SELECT。{{raw}} を読み込み元に置き換えて使う。
//
//go:embed transactions.sql
var transactionsSQL string

// Run は対象月の raw CSV を読み、明細を Parquet で書き出す。書き出した件数を返す。
func Run(ctx context.Context, l *lake.Lake, m month.Month) (int64, string, error) {
	src := l.RawCSV(m)
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
