package ledger

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
)

// 集計の期間と行の上限。月なら2年、日なら maxDays まで並べれば、前の年との比較に足りる。
const (
	maxAggregateMonths   = 24
	defaultAggregateRows = 100
	maxAggregateRows     = 500
)

// dimensions は集計の切り口と、それを引く SQL の式。
var dimensions = map[string]string{
	"month":       "month",
	"date":        "strftime(date, '%Y-%m-%d')",
	"category":    "coalesce(category, '未分類')",
	"subcategory": "coalesce(subcategory, '未分類')",
	"account":     "coalesce(account, '未設定')",
}

// AggregateQuery は集計の条件。From / To は日付（YYYY-MM-DD、どちらも含む）。
type AggregateQuery struct {
	GroupBy           []string
	From, To          string
	Kind              string
	Categories        []string
	ExcludeCategories []string
	Accounts          []string
	Keyword           string
}

// aggregateRecord は切り口の値の組み合わせ1つと、その合計。
type aggregateRecord struct {
	keys   map[string]string
	amount int64
	count  int64
}

// Aggregate は条件に合う収入か支出を、切り口ごとに合計する。金額は正の値。
func (s *Store) Aggregate(ctx context.Context, q AggregateQuery) ([]aggregateRecord, error) {
	where := []string{"kind = ?", "date BETWEEN ?::DATE AND ?::DATE"}
	args := []any{q.Kind, q.From, q.To}
	in := func(expr string, values []string, not bool) {
		if len(values) == 0 {
			return
		}
		op := " IN "
		if not {
			op = " NOT IN "
		}
		where = append(where, expr+op+"(?"+strings.Repeat(", ?", len(values)-1)+")")
		for _, v := range values {
			args = append(args, v)
		}
	}
	in("category", q.Categories, false)
	in("coalesce(category, '')", q.ExcludeCategories, true)
	in("account", q.Accounts, false)
	if q.Keyword != "" {
		where = append(where, "(description ILIKE ? OR memo ILIKE ?)")
		kw := "%" + q.Keyword + "%"
		args = append(args, kw, kw)
	}

	cols := make([]string, len(q.GroupBy))
	for i, d := range q.GroupBy {
		cols[i] = dimensions[d]
	}
	// 支出は負で入っているので、向きを変えて正の値にする。返金で減った分は差し引かれる。
	sel := append(slices.Clone(cols), "(CASE WHEN kind = 'expense' THEN -sum(amount) ELSE sum(amount) END)::BIGINT", "count(*)")
	group := append(slices.Clone(cols), "kind")

	rows, ok, err := s.query(ctx, `
SELECT `+strings.Join(sel, ", ")+`
FROM `+s.transactions()+`
WHERE `+strings.Join(where, " AND ")+`
GROUP BY `+strings.Join(group, ", "), args...)
	if err != nil || !ok {
		return nil, err
	}
	defer rows.Close()

	var out []aggregateRecord
	for rows.Next() {
		keys := make([]string, len(q.GroupBy))
		dest := make([]any, 0, len(keys)+2)
		for i := range keys {
			dest = append(dest, &keys[i])
		}
		r := aggregateRecord{keys: map[string]string{}}
		dest = append(dest, &r.amount, &r.count)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for i, d := range q.GroupBy {
			r.keys[d] = keys[i]
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) AggregateTransactions(ctx context.Context, req *connect.Request[finlakev1.AggregateTransactionsRequest]) (*connect.Response[finlakev1.AggregateTransactionsResponse], error) {
	in := req.Msg
	period := ""
	seen := map[string]bool{}
	for _, d := range in.GetGroupBy() {
		if _, ok := dimensions[d]; !ok {
			return nil, invalid("group_by must be month, date, category, subcategory or account: %q", d)
		}
		if seen[d] {
			return nil, invalid("group_by has %s twice", d)
		}
		seen[d] = true
		if d == "month" || d == "date" {
			if period != "" {
				return nil, invalid("group_by takes month or date, not both")
			}
			period = d
		}
	}
	kind := in.GetKind()
	if kind == "" {
		kind = "expense"
	}
	if kind != "expense" && kind != "income" {
		return nil, invalid("kind must be expense or income")
	}
	limit := int(in.GetLimit())
	if limit <= 0 {
		limit = defaultAggregateRows
	}
	if limit > maxAggregateRows {
		return nil, invalid("limit must be at most %d", maxAggregateRows)
	}

	months, err := s.store.Months(ctx)
	if err != nil {
		return nil, err
	}
	if len(months) == 0 {
		return connect.NewResponse(&finlakev1.AggregateTransactionsResponse{}), nil
	}
	from, to, err := aggregatePeriod(in.From, in.To, months[0].GetMonth())
	if err != nil {
		return nil, err
	}
	switch period {
	case "month":
		if monthsBetween(from, to) > maxAggregateMonths {
			return nil, invalid("the period must be at most %d months with month in group_by", maxAggregateMonths)
		}
	case "date":
		if err := checkPeriod(from, to, maxDays); err != nil {
			return nil, err
		}
	}

	records, err := s.store.Aggregate(ctx, AggregateQuery{
		GroupBy: in.GetGroupBy(), From: from, To: to, Kind: kind,
		Categories: in.GetCategories(), ExcludeCategories: in.GetExcludeCategories(), Accounts: in.GetAccounts(), Keyword: in.GetKeyword(),
	})
	if err != nil {
		return nil, err
	}
	ingested := map[string]bool{}
	for _, m := range months {
		ingested[m.GetMonth()] = true
	}
	return connect.NewResponse(tabulate(records, in.GetGroupBy(), period, periodsOf(period, from, to, ingested), limit)), nil
}

// aggregatePeriod は from / to（YYYY-MM か YYYY-MM-DD）を日付に直す。省いた to は latest（YYYY-MM）の末日、
// 省いた from は to の月から数えて12か月目の初日。
func aggregatePeriod(fromArg, toArg *string, latest string) (string, string, error) {
	to, err := periodBound("to", toArg, latest, true)
	if err != nil {
		return "", "", err
	}
	t, _ := time.Parse(time.DateOnly, to)
	from, err := periodBound("from", fromArg, t.AddDate(0, 0, 1-t.Day()).AddDate(0, -11, 0).Format("2006-01"), false)
	if err != nil {
		return "", "", err
	}
	if err := checkPeriod(from, to, 0); err != nil {
		return "", "", err
	}
	return from, to, nil
}

// periodBound は月なら初日か末日（end）に、日付ならそのまま返す。v が無ければ fallback（YYYY-MM）を使う。
func periodBound(name string, v *string, fallback string, end bool) (string, error) {
	s := fallback
	if v != nil {
		s = *v
	}
	if monthPattern.MatchString(s) {
		first, _ := time.Parse("2006-01", s)
		if end {
			return first.AddDate(0, 1, -1).Format(time.DateOnly), nil
		}
		return first.Format(time.DateOnly), nil
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return "", invalid("%s must be YYYY-MM or YYYY-MM-DD: %q", name, s)
	}
	return s, nil
}

// monthsBetween は from から to（日付、どちらも含む）がかかる月の数。
func monthsBetween(from, to string) int {
	f, _ := time.Parse(time.DateOnly, from)
	t, _ := time.Parse(time.DateOnly, to)
	return (t.Year()-f.Year())*12 + int(t.Month()-f.Month()) + 1
}

// periodsOf は表の列にする月か日を古い順に返す。取り込んでいない月にかかる列は作らない。
func periodsOf(period, from, to string, ingested map[string]bool) []string {
	if period == "" {
		return nil
	}
	f, _ := time.Parse(time.DateOnly, from)
	t, _ := time.Parse(time.DateOnly, to)
	var out []string
	for d := f; !d.After(t); d = d.AddDate(0, 0, 1) {
		m := d.Format("2006-01")
		if !ingested[m] {
			continue
		}
		switch {
		case period == "date":
			out = append(out, d.Format(time.DateOnly))
		case len(out) == 0 || out[len(out)-1] != m:
			out = append(out, m)
		}
	}
	return out
}

// tabulate は切り口ごとの合計を、period（month か date、無ければ空）を列にした表にまとめる。
func tabulate(records []aggregateRecord, groupBy []string, period string, periods []string, limit int) *finlakev1.AggregateTransactionsResponse {
	column := map[string]int{}
	for i, p := range periods {
		column[p] = i
	}
	res := &finlakev1.AggregateTransactionsResponse{Periods: periods, PeriodTotals: make([]int64, len(periods))}
	byKey := map[string]*finlakev1.AggregateRow{}
	var rows []*finlakev1.AggregateRow
	for _, r := range records {
		var key []string
		for _, d := range groupBy {
			if d != period {
				key = append(key, r.keys[d])
			}
		}
		row := byKey[strings.Join(key, "\x00")]
		if row == nil {
			row = &finlakev1.AggregateRow{Amounts: make([]int64, len(periods))}
			for _, d := range groupBy {
				v := r.keys[d]
				switch d {
				case "category":
					row.Category = &v
				case "subcategory":
					row.Subcategory = &v
				case "account":
					row.Account = &v
				}
			}
			byKey[strings.Join(key, "\x00")] = row
			rows = append(rows, row)
		}
		if i, ok := column[r.keys[period]]; ok {
			row.Amounts[i] += r.amount
			res.PeriodTotals[i] += r.amount
		}
		row.Total += r.amount
		row.Count += r.count
		res.Total += r.amount
		res.Count += r.count
	}
	for _, row := range rows {
		if len(periods) > 0 {
			row.Stats = statsOf(row.Amounts)
		}
	}
	slices.SortStableFunc(rows, func(a, b *finlakev1.AggregateRow) int {
		if c := cmp.Compare(b.Total, a.Total); c != 0 {
			return c
		}
		return cmp.Compare(a.GetCategory()+"\x00"+a.GetSubcategory()+"\x00"+a.GetAccount(), b.GetCategory()+"\x00"+b.GetSubcategory()+"\x00"+b.GetAccount())
	})
	if len(rows) > limit {
		for _, row := range rows[limit:] {
			res.OmittedRows++
			res.OmittedTotal += row.Total
		}
		rows = rows[:limit]
	}
	res.Rows = rows
	return res
}

func statsOf(amounts []int64) *finlakev1.PeriodStats {
	sorted := slices.Sorted(slices.Values(amounts))
	n := len(sorted)
	median := sorted[n/2]
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return &finlakev1.PeriodStats{Median: median, Min: sorted[0], Max: sorted[n-1]}
}
