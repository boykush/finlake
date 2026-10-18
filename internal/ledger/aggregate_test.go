package ledger

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/ledger/ledgertest"
)

func aggregate(t *testing.T, svc *Service, req *finlakev1.AggregateTransactionsRequest) *finlakev1.AggregateTransactionsResponse {
	t.Helper()
	res, err := svc.AggregateTransactions(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func TestAggregateTransactions(t *testing.T) {
	svc := NewService(NewStore(ledgertest.Lake(t)))

	// 大項目ごとの月の推移。明細の無い月は 0 で、中央値にも 0 として入る
	res := aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"month", "category"}, From: ptr("2026-09"), To: ptr("2026-10")})
	if !slices.Equal(res.GetPeriods(), []string{"2026-09", "2026-10"}) || len(res.GetRows()) != 2 {
		t.Fatalf("by month and category = %v", res)
	}
	utilities, food := res.GetRows()[0], res.GetRows()[1]
	if utilities.GetCategory() != "水道・光熱費" || !slices.Equal(utilities.GetAmounts(), []int64{8000, 0}) || utilities.GetStats().GetMedian() != 4000 || utilities.GetStats().GetMin() != 0 {
		t.Errorf("utilities = %v", utilities)
	}
	if food.GetCategory() != "食費" || !slices.Equal(food.GetAmounts(), []int64{3200, 4000}) || food.GetTotal() != 7200 || food.GetCount() != 2 || food.Subcategory != nil {
		t.Errorf("food = %v", food)
	}
	// 振替・計算対象外・収入は入らない
	if !slices.Equal(res.GetPeriodTotals(), []int64{11200, 4000}) || res.GetTotal() != 15200 || res.GetCount() != 3 {
		t.Errorf("totals = %v, %d, %d", res.GetPeriodTotals(), res.GetTotal(), res.GetCount())
	}

	// 期間を省くと最後の月から12か月。取り込んでいない月は列にならない
	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"month"}})
	if !slices.Equal(res.GetPeriods(), []string{"2026-09", "2026-10"}) || len(res.GetRows()) != 1 || !slices.Equal(res.GetRows()[0].GetAmounts(), []int64{11200, 4000}) {
		t.Errorf("by month over the default period = %v", res)
	}

	// 時間の切り口が無ければ、期間全体の合計の行だけ
	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"category", "subcategory"}, From: ptr("2026-09"), To: ptr("2026-09")})
	if len(res.GetPeriods()) != 0 || len(res.GetRows()) != 2 || res.GetRows()[0].GetSubcategory() != "電気代" || res.GetRows()[1].GetSubcategory() != "食料品" || res.GetRows()[0].Stats != nil {
		t.Errorf("by category and subcategory = %v", res)
	}

	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"subcategory"}, Kind: ptr("income")})
	if len(res.GetRows()) != 1 || res.GetRows()[0].GetSubcategory() != "給与" || res.GetTotal() != 300000 {
		t.Errorf("income by subcategory = %v", res)
	}

	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"date"}, From: ptr("2026-09-01"), To: ptr("2026-09-05")})
	if len(res.GetPeriods()) != 5 || !slices.Equal(res.GetRows()[0].GetAmounts(), []int64{0, 0, 3200, 0, 8000}) || res.GetRows()[0].GetStats().GetMedian() != 0 {
		t.Errorf("by date = %v", res)
	}

	// 上限で切った行も全体の合計には入る
	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"category"}, Limit: ptr[int32](1)})
	if len(res.GetRows()) != 1 || res.GetOmittedRows() != 1 || res.GetOmittedTotal() != 7200 || res.GetTotal() != 15200 {
		t.Errorf("limited = %v", res)
	}

	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"account"}, ExcludeCategories: []string{"水道・光熱費"}, Keyword: ptr("スーパー")})
	if len(res.GetRows()) != 1 || res.GetRows()[0].GetAccount() != "カード" || res.GetTotal() != 7200 {
		t.Errorf("filtered = %v", res)
	}
	res = aggregate(t, svc, &finlakev1.AggregateTransactionsRequest{Accounts: []string{"銀行"}})
	if len(res.GetRows()) != 0 || res.GetTotal() != 0 {
		t.Errorf("expenses from the bank (only a transfer) = %v", res)
	}
}

func TestAggregateTransactionsRejects(t *testing.T) {
	svc := NewService(NewStore(ledgertest.Lake(t)))
	for name, req := range map[string]*finlakev1.AggregateTransactionsRequest{
		"unknown dimension": {GroupBy: []string{"week"}},
		"month and date":    {GroupBy: []string{"month", "date"}},
		"twice":             {GroupBy: []string{"category", "category"}},
		"kind":              {Kind: ptr("transfer")},
		"limit":             {Limit: ptr[int32](501)},
		"25 months":         {GroupBy: []string{"month"}, From: ptr("2024-10"), To: ptr("2026-10")},
		"401 days":          {GroupBy: []string{"date"}, From: ptr("2025-09-01"), To: ptr("2026-10-06")},
		"not a date":        {From: ptr("2026/09")},
		"reversed":          {From: ptr("2026-10"), To: ptr("2026-09")},
	} {
		if _, err := svc.AggregateTransactions(context.Background(), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// 24 か月ちょうどは通る
	if _, err := svc.AggregateTransactions(context.Background(), connect.NewRequest(&finlakev1.AggregateTransactionsRequest{GroupBy: []string{"month"}, From: ptr("2024-11"), To: ptr("2026-10")})); err != nil {
		t.Errorf("24 months: err = %v", err)
	}
}

func TestAggregateTransactionsOnAnEmptyLake(t *testing.T) {
	l, err := lake.Open(context.Background(), lake.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	res := aggregate(t, NewService(NewStore(l)), &finlakev1.AggregateTransactionsRequest{GroupBy: []string{"month"}})
	if len(res.GetPeriods()) != 0 || len(res.GetRows()) != 0 {
		t.Errorf("empty = %v", res)
	}
}
