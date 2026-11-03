// Package ledgertest はサンプルの明細を入れたデータレイクをテストに渡す。
package ledgertest

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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

// put は month の CSV を raw 層に置き、product まで作る。
func put(t *testing.T, l *lake.Lake, m month.Month, csv string) {
	t.Helper()
	p := l.Path("raw", "moneyforward", "month="+m.String(), "収入・支出詳細.csv")
	if err := l.PrepareWrite(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transform.Run(context.Background(), l, m); err != nil {
		t.Fatal(err)
	}
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

	put(t, l, month.Month{Year: 2026, Month: time.September}, sep)
	put(t, l, month.Month{Year: 2026, Month: time.October}, oct)
	return l
}

// AddMonth は month（YYYY-MM）の明細を1件、product まで作る。
func AddMonth(t *testing.T, l *lake.Lake, m string) {
	t.Helper()
	mm, err := month.Parse(m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	csv := `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"
"1","` + strings.ReplaceAll(m, "-", "/") + `/02","スーパー","-1000","カード","食費","食料品","","0","` + m + `"
`
	put(t, l, mm, csv)
}
