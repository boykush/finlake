// Package ingest はマネーフォワード ME の CSV をデータレイクの raw 層に取り込む。
package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/month"
)

// Downloader は対象月の CSV（UTF-8）を返す。
type Downloader interface {
	DownloadCSV(ctx context.Context, m month.Month) ([]byte, error)
}

// Run は対象月の CSV を取得し、raw 層に置く。同じ月を取り込み直すと上書きする。
// 置いたパスを返す。
func Run(ctx context.Context, l *lake.Lake, d Downloader, m month.Month) (string, error) {
	body, err := d.DownloadCSV(ctx, m)
	if err != nil {
		return "", err
	}

	// DuckDB の COPY で書き出すため、一度ローカルに置く。S3 にもローカルにも同じ経路で書ける。
	dir, err := os.MkdirTemp("", "finlake-ingest-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "transactions.csv")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}

	dst := l.RawCSV(m)
	if err := l.PrepareWrite(dst); err != nil {
		return "", err
	}
	// raw 層は加工しない。型を推論させず、全列を文字列のまま写す。
	q := fmt.Sprintf(
		"COPY (SELECT * FROM read_csv(%s, header = true, all_varchar = true)) TO %s (FORMAT csv, HEADER true)",
		lake.Quote(tmp), lake.Quote(dst),
	)
	if _, err := l.DB.ExecContext(ctx, q); err != nil {
		return "", fmt.Errorf("write raw csv: %w", err)
	}
	return dst, nil
}
