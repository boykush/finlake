package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"

	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/moneyforward"
	"github.com/boykush/finlake/internal/month"
)

const header = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"` + "\n"

func row(date, id string) string {
	return fmt.Sprintf(`"1","%s","スーパー","-3200","楽天カード","食費","食料品","","0","%s"`+"\n", date, id)
}

func shiftJIS(t *testing.T, s string) []byte {
	t.Helper()
	b, err := japanese.ShiftJIS.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(b)
}

func openTestLake(t *testing.T) *lake.Lake {
	t.Helper()
	l, err := lake.Open(context.Background(), lake.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// loadedMonths は product にある月と件数を "2026-09:2" の形で返す。
func loadedMonths(t *testing.T, l *lake.Lake) []string {
	t.Helper()
	q := fmt.Sprintf(
		"SELECT month || ':' || count(*) FROM read_parquet(%s, hive_partitioning = true) GROUP BY month ORDER BY month",
		lake.Quote(l.TransactionsGlob()),
	)
	rows, err := l.DB.QueryContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBackfill(t *testing.T) {
	backfillInterval = 0
	// 2026-07 だけ明細が無い。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := header
		if from := r.URL.Query().Get("from"); from != "2026/07/01" {
			body += row(from, "a") + row(strings.TrimSuffix(from, "01")+"15", "b")
		}
		_, _ = w.Write(shiftJIS(t, body))
	}))
	defer srv.Close()

	l := openTestLake(t)
	client := moneyforward.NewClient("_moneybook_session=x")
	client.BaseURL = srv.URL
	from := month.Month{Year: 2026, Month: time.June}
	to := month.Month{Year: 2026, Month: time.August}
	if err := backfill(context.Background(), l, client, from, to); err != nil {
		t.Fatal(err)
	}

	if got, want := loadedMonths(t, l), []string{"2026-06:2", "2026-08:2"}; !slices.Equal(got, want) {
		t.Errorf("loaded = %v, want %v", got, want)
	}
}

func TestSyncPending(t *testing.T) {
	ctx := context.Background()
	l := openTestLake(t)
	put := func(folder, body string) {
		p := l.Path("raw", "moneyforward", folder, "収入・支出詳細.csv")
		if err := l.PrepareWrite(p); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, shiftJIS(t, body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("month=2026-08", header+row("2026/08/01", "a"))
	put("month=2026-09", header+row("2026/09/01", "a")+row("2026/09/02", "b"))
	// 置き間違い: フォルダは 10 月、明細は 11 月。
	put("month=2026-10", header+row("2026/11/01", "a"))

	if err := syncPending(ctx, l, true); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := os.Stat(l.TransactionsParquet(month.Month{Year: 2026, Month: time.August})); err == nil {
		t.Fatal("dry run must not write")
	}

	// 置き間違いの月は失敗として返し、ほかの月は変換する。
	if err := syncPending(ctx, l, false); err == nil {
		t.Error("want an error for the misplaced month")
	}
	if got, want := loadedMonths(t, l), []string{"2026-08:1", "2026-09:2"}; !slices.Equal(got, want) {
		t.Errorf("loaded = %v, want %v", got, want)
	}
}
