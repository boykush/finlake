package ledger

import (
	"context"
	"testing"

	"github.com/boykush/finlake/internal/lake"
	"github.com/boykush/finlake/internal/ledger/ledgertest"
)

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
	s := NewStore(ledgertest.Lake(t))

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
	if len(txs) != 1 || txs[0].Id != "a2" || txs[0].Date != "2026-09-03" || txs[0].Amount != -3200 {
		t.Errorf("transactions = %+v", txs)
	}
	txs, _ = s.Transactions(ctx, TransactionFilter{Month: "2026-09", Kind: "transfer", Limit: 10})
	if len(txs) != 1 || txs[0].Id != "a4" {
		t.Errorf("transfers = %+v", txs)
	}
	// 内容が空の明細が混じっても、月の明細は返る
	txs, err = s.Transactions(ctx, TransactionFilter{Month: "2026-09", Category: "食費", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 2 || txs[1].Id != "a6" || txs[1].Description != nil {
		t.Errorf("transactions without a description = %+v", txs)
	}

	trend, err := s.CategoryTrend(ctx, "expense", "食費", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 2 || trend[0].Amount != 3200 || trend[1].Amount != 4000 {
		t.Errorf("trend = %+v", trend)
	}
}
