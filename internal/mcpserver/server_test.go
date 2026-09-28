package mcpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
"1","2026/09/03","スーパー","-3200","カード","食費","食料品","週末の買い出し","0","a2"
`

const oct = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"
"1","2026/10/02","スーパー","-4000","カード","食費","食料品","","0","b1"
`

type fakeDownloader map[string]string

func (f fakeDownloader) DownloadCSV(_ context.Context, m month.Month) ([]byte, error) {
	return []byte(f[m.String()]), nil
}

func setup(t *testing.T) *Store {
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
	return NewStore(l)
}

func TestEmptyLake(t *testing.T) {
	l, err := lake.Open(context.Background(), lake.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	months, err := NewStore(l).Months(context.Background())
	if err != nil || len(months) != 0 {
		t.Fatalf("months = %v, err = %v", months, err)
	}
}

func TestStore(t *testing.T) {
	ctx := context.Background()
	s := setup(t)

	months, err := s.Months(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 2 || months[0].Month != "2026-10" {
		t.Fatalf("months = %+v", months)
	}
	sepTotal := months[1]
	// 振替と計算対象外は収支に入らず、重複した a2 は1件になる
	if sepTotal.Income != 300000 || sepTotal.Expense != 11200 || sepTotal.Balance != 288800 || sepTotal.Count != 3 {
		t.Errorf("2026-09 = %+v", sepTotal)
	}

	sum, err := s.Summary(ctx, "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if sum == nil || len(sum.Categories) != 3 || sum.Categories[1].Category != "水道・光熱費" {
		t.Errorf("summary = %+v", sum)
	}
	if sum, _ := s.Summary(ctx, "2020-01"); sum != nil {
		t.Errorf("summary of an empty month = %+v", sum)
	}

	txs, err := s.Transactions(ctx, TransactionFilter{Month: "2026-09", Keyword: "買い出し", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 1 || txs[0].ID != "a2" || txs[0].Date != "2026-09-03" || txs[0].Amount != -3200 {
		t.Errorf("transactions = %+v", txs)
	}
	txs, _ = s.Transactions(ctx, TransactionFilter{Month: "2026-09", Kind: "transfer", Limit: 10})
	if len(txs) != 1 || txs[0].ID != "a4" {
		t.Errorf("transfers = %+v", txs)
	}

	trend, err := s.CategoryTrend(ctx, "expense", "食費", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 2 || trend[0].Amount != 3200 || trend[1].Amount != 4000 {
		t.Errorf("trend = %+v", trend)
	}
}

func post(t *testing.T, url, token, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func TestHandlerAuth(t *testing.T) {
	s := setup(t)
	srv := httptest.NewServer(Handler(NewServer(s, "test"), []string{"secret-1", "secret-2"}))
	defer srv.Close()

	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_monthly_summary","arguments":{"month":"2026-09"}}}`

	if resp, _ := post(t, srv.URL+"/mcp", "", call); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv.URL+"/mcp", "wrong", call); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d", resp.StatusCode)
	}

	resp, body := post(t, srv.URL+"/mcp", "secret-2", call)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if strings.Contains(body, `"isError":true`) || !strings.Contains(body, `"income":300000`) {
		t.Errorf("body = %s", body)
	}

	health, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d", health.StatusCode)
	}
}
