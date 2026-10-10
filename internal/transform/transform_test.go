package transform

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"

	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/month"
)

const header = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"` + "\n"

func row(date, id string) string {
	return `"1","` + date + `","スーパー","-3200","楽天カード","食費","食料品","","0","` + id + `"` + "\n"
}

func shiftJIS(t *testing.T, s string) []byte {
	t.Helper()
	b, err := japanese.ShiftJIS.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(b)
}

func openLake(t *testing.T) (*lake.Lake, string) {
	t.Helper()
	root := t.TempDir()
	l, err := lake.Open(context.Background(), lake.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, root
}

// put は raw 層に手でファイルを置いたのと同じ状態を作る。
func put(t *testing.T, root, folder, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(root, "raw", "moneyforward", folder, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var sep = month.Month{Year: 2026, Month: time.September}

func TestRunReadsDownloadedFileAsIs(t *testing.T) {
	l, root := openLake(t)
	body := header + row("2026/09/24", "a") + row("2026/09/01", "b")
	put(t, root, "month=2026-09", "収入・支出詳細_2026-09-01_2026-09-30.csv", shiftJIS(t, body))

	n, _, err := Run(context.Background(), l, sep)
	if err != nil || n != 2 {
		t.Fatalf("rows = %d, err = %v", n, err)
	}
}

func TestRunRejectsMisplacedFiles(t *testing.T) {
	tests := map[string]func(root string){
		"no file": func(string) {},
		"two files": func(root string) {
			put(t, root, "month=2026-09", "a.csv", []byte(header+row("2026/09/01", "a")))
			put(t, root, "month=2026-09", "b.csv", []byte(header+row("2026/09/02", "b")))
		},
		"another month": func(root string) {
			put(t, root, "month=2026-09", "a.csv", []byte(header+row("2026/10/01", "a")))
		},
		"two months": func(root string) {
			put(t, root, "month=2026-09", "a.csv", []byte(header+row("2026/09/30", "a")+row("2026/10/01", "b")))
		},
		"not a mf csv": func(root string) {
			put(t, root, "month=2026-09", "a.csv", []byte("date,amount\n2026/09/30,1\n"))
		},
	}
	for name, arrange := range tests {
		l, root := openLake(t)
		arrange(root)
		if _, _, err := Run(context.Background(), l, sep); err == nil {
			t.Errorf("%s: want an error", name)
		}
		if _, err := os.Stat(l.TransactionsParquet(sep)); err == nil {
			t.Errorf("%s: product must not be written", name)
		}
	}
}

func TestPending(t *testing.T) {
	ctx := context.Background()
	l, root := openLake(t)
	pending := func() []string {
		t.Helper()
		months, err := Pending(ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range months {
			got = append(got, m.String())
		}
		return got
	}

	if got := pending(); len(got) != 0 {
		t.Fatalf("empty lake: pending = %v", got)
	}

	raw := put(t, root, "month=2026-09", "a.csv", []byte(header+row("2026/09/01", "a")))
	put(t, root, "month=2026-08", "a.csv", []byte(header+row("2026/08/01", "a")))
	if got, want := pending(), []string{"2026-08", "2026-09"}; !slices.Equal(got, want) {
		t.Fatalf("pending = %v, want %v", got, want)
	}

	for _, m := range []month.Month{sep, sep.Add(-1)} {
		if _, _, err := Run(ctx, l, m); err != nil {
			t.Fatal(err)
		}
	}
	if got := pending(); len(got) != 0 {
		t.Fatalf("after transform: pending = %v", got)
	}

	// 同じ月を置き直すと、また対象になる。
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(raw, later, later); err != nil {
		t.Fatal(err)
	}
	if got, want := pending(), []string{"2026-09"}; !slices.Equal(got, want) {
		t.Fatalf("after replacing raw: pending = %v, want %v", got, want)
	}
}

func TestPendingReportsMisnamedFolders(t *testing.T) {
	l, root := openLake(t)
	put(t, root, "month=2026-09", "a.csv", []byte(header+row("2026/09/01", "a")))
	put(t, root, "2026-10", "a.csv", []byte(header+row("2026/10/01", "a")))
	put(t, root, "month=2026-1", "a.csv", []byte(header+row("2026/01/01", "a")))

	months, err := Pending(context.Background(), l)
	if err == nil {
		t.Error("want an error for the misnamed folders")
	}
	if len(months) != 1 || months[0] != sep {
		t.Errorf("pending = %v, want only 2026-09", months)
	}
}
