// Package ledgertest はサンプルの明細を入れたデータレイクをテストに渡す。
package ledgertest

import (
	"context"
	"testing"
	"time"

	"github.com/boykush/finlake/internal/ingest"
	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/month"
	"github.com/boykush/finlake/internal/transform"
)

const sep = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"
"1","2026/09/01","給与","300,000","銀行","収入","給与","","0","a1"
"1","2026/09/03","スーパー","-3200","カード","食費","食料品","週末の買い出し","0","a2"
"1","2026/09/05","電気代","-8000","カード","水道・光熱費","電気代","","0","a3"
"1","2026/09/10","カード引き落とし","-11200","銀行","未分類","未分類","","1","a4"
"0","2026/09/12","立替","-5000","カード","交際費","","","0","a5"
"0","2026/09/20","","-1500","カード","食費","外食","","0","a6"
"1","2026/09/03","スーパー","-3200","カード","食費","食料品","週末の買い出し","0","a2"
`

const oct = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"
"1","2026/10/02","スーパー","-4000","カード","食費","食料品","","0","b1"
`

type fakeDownloader map[string]string

func (f fakeDownloader) DownloadCSV(_ context.Context, m month.Month) ([]byte, error) {
	return []byte(f[m.String()]), nil
}

// Lake は 2026-09 と 2026-10 の明細を product まで作ったデータレイクを返す。
func Lake(t *testing.T) *lake.Lake {
	t.Helper()
	ctx := context.Background()
	l, err := lake.Open(ctx, lake.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	d := fakeDownloader{"2026-09": sep, "2026-10": oct}
	for _, m := range []month.Month{{Year: 2026, Month: time.September}, {Year: 2026, Month: time.October}} {
		if _, err := ingest.Run(ctx, l, d, m); err != nil {
			t.Fatal(err)
		}
		if _, _, err := transform.Run(ctx, l, m); err != nil {
			t.Fatal(err)
		}
	}
	return l
}
