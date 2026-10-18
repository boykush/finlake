package ledger

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/internal/ledger/ledgertest"
)

func TestService(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewStore(ledgertest.Lake(t)))

	sum, err := svc.GetMonthlySummary(ctx, connect.NewRequest(&finlakev1.GetMonthlySummaryRequest{Month: "2026-09"}))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Msg.GetIncome() != 300000 || len(sum.Msg.GetCategories()) != 3 {
		t.Errorf("summary = %v", sum.Msg)
	}

	// limit を省くと既定の件数で返る
	txs, err := svc.ListTransactions(ctx, connect.NewRequest(&finlakev1.ListTransactionsRequest{Month: ptr("2026-09")}))
	if err != nil || len(txs.Msg.GetTransactions()) != 6 {
		t.Errorf("transactions = %v, err = %v", txs, err)
	}

	for month, want := range map[string]connect.Code{"2020-01": connect.CodeNotFound, "2026-9": connect.CodeInvalidArgument} {
		_, err := svc.GetMonthlySummary(ctx, connect.NewRequest(&finlakev1.GetMonthlySummaryRequest{Month: month}))
		if got := connect.CodeOf(err); got != want {
			t.Errorf("GetMonthlySummary(%s) code = %v, want %v", month, got, want)
		}
	}

	for name, req := range map[string]*finlakev1.ListTransactionsRequest{
		"no period":  {},
		"both":       {Month: ptr("2026-09"), From: ptr("2026-09-01"), To: ptr("2026-09-30")},
		"reversed":   {From: ptr("2026-09-30"), To: ptr("2026-09-01")},
		"not a date": {From: ptr("2026-09"), To: ptr("2026-09-30")},
	} {
		if _, err := svc.ListTransactions(ctx, connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("ListTransactions(%s) err = %v", name, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }
