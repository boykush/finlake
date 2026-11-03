package moneyforward

import (
	"slices"
	"strings"
	"testing"
)

const sampleCSV = `"計算対象","日付","内容","金額（円）","保有金融機関","大項目","中項目","メモ","振替","ID"
"1","2026/09/24","スーパー","-3200","楽天カード","食費","食料品","","0","abc123"
`

func TestMonths(t *testing.T) {
	header := strings.SplitAfterN(sampleCSV, "\n", 2)[0]
	row := func(date string) string {
		return `"1","` + date + `","スーパー","-3200","楽天カード","食費","食料品","","0","x"` + "\n"
	}
	tests := []struct {
		name string
		csv  string
		want []string
	}{
		{"no rows", header, nil},
		{"one month", header + row("2026/09/24") + row("2026/09/01"), []string{"2026-09"}},
		{"sorted", header + row("2026/10/01") + row("2025/12/31"), []string{"2025-12", "2026-10"}},
	}
	for _, tt := range tests {
		months, err := Months([]byte(tt.csv))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var got []string
		for _, m := range months {
			got = append(got, m.String())
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}

	if _, err := Months([]byte(header + row("2026-09-24"))); err == nil {
		t.Error("invalid date should fail")
	}
}
