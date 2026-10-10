// Package month は家計簿の対象月を表す。
package month

import (
	"fmt"
	"time"
)

// JST は家計簿の月の境界を決めるタイムゾーン。
// distroless には tzdata が無いので LoadLocation に頼らず固定オフセットで持つ。
var JST = time.FixedZone("Asia/Tokyo", 9*60*60)

// Month は年と月の組。
type Month struct {
	Year  int
	Month time.Month
}

// Parse は "2026-09" / "current" / "previous" を受け取る。
// current と previous は now を JST で見たときの月を基準にする。
func Parse(s string, now time.Time) (Month, error) {
	switch s {
	case "", "current":
		return Of(now), nil
	case "previous":
		return Of(now).Add(-1), nil
	}
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return Month{}, fmt.Errorf("invalid month %q (want YYYY-MM, current or previous)", s)
	}
	return Month{Year: t.Year(), Month: t.Month()}, nil
}

// Of は t を JST で見たときの月を返す。
func Of(t time.Time) Month {
	t = t.In(JST)
	return Month{Year: t.Year(), Month: t.Month()}
}

// Add は n か月後の月を返す。
func (m Month) Add(n int) Month {
	t := time.Date(m.Year, m.Month+time.Month(n), 1, 0, 0, 0, 0, JST)
	return Month{Year: t.Year(), Month: t.Month()}
}

// Compare は m が o より前なら負、同じなら 0、後なら正を返す。
func (m Month) Compare(o Month) int {
	return (m.Year*12 + int(m.Month)) - (o.Year*12 + int(o.Month))
}

// String は "2026-09" 形式。データレイクのパーティション名にも使う。
func (m Month) String() string {
	return fmt.Sprintf("%04d-%02d", m.Year, int(m.Month))
}
