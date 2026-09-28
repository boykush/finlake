package moneyforward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"

	"github.com/boykush/finlake/internal/month"
)

const sampleCSV = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"
"1","2026/09/24","スーパー","-3200","楽天カード","食費","食料品","","0","abc123"
`

func shiftJIS(t *testing.T, s string) []byte {
	t.Helper()
	b, err := japanese.ShiftJIS.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(b)
}

func TestDownloadCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cf/csv" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("from") != "2026/09/01" || q.Get("month") != "9" || q.Get("year") != "2026" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if r.Header.Get("Cookie") != "_moneybook_session=x" {
			t.Errorf("cookie = %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write(shiftJIS(t, sampleCSV))
	}))
	defer srv.Close()

	c := NewClient("_moneybook_session=x")
	c.BaseURL = srv.URL
	got, err := c.DownloadCSV(context.Background(), month.Month{Year: 2026, Month: time.September})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sampleCSV {
		t.Errorf("got %q", got)
	}
}

func TestDownloadCSVSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sign_in", http.StatusFound)
	}))
	defer srv.Close()

	c := NewClient("_moneybook_session=expired")
	c.BaseURL = srv.URL
	_, err := c.DownloadCSV(context.Background(), month.Month{Year: 2026, Month: time.September})
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
}

func TestDecodeRejectsHTML(t *testing.T) {
	_, err := Decode([]byte("<!DOCTYPE html><html>" + strings.Repeat("x", 10)))
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
}
