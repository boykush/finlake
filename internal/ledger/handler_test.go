package ledger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/gen/finlake/v1/finlakev1connect"
	"github.com/boykush/finlake/internal/ledger/ledgertest"
)

func TestHandler(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(Handler(NewService(NewStore(ledgertest.Lake(t)))))
	defer srv.Close()
	client := finlakev1connect.NewLedgerServiceClient(http.DefaultClient, srv.URL)

	sum, err := client.GetMonthlySummary(ctx, connect.NewRequest(&finlakev1.GetMonthlySummaryRequest{Month: "2026-09"}))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Msg.GetIncome() != 300000 || len(sum.Msg.GetCategories()) != 3 {
		t.Errorf("summary = %v", sum.Msg)
	}

	// limit を省くと既定の件数で返る
	txs, err := client.ListTransactions(ctx, connect.NewRequest(&finlakev1.ListTransactionsRequest{Month: ptr("2026-09")}))
	if err != nil || len(txs.Msg.GetTransactions()) != 6 {
		t.Errorf("transactions = %v, err = %v", txs, err)
	}

	for month, want := range map[string]connect.Code{"2020-01": connect.CodeNotFound, "2026-9": connect.CodeInvalidArgument} {
		_, err := client.GetMonthlySummary(ctx, connect.NewRequest(&finlakev1.GetMonthlySummaryRequest{Month: month}))
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
		if _, err := client.ListTransactions(ctx, connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("ListTransactions(%s) err = %v", name, err)
		}
	}
	if _, err := client.ListDailyExpenses(ctx, connect.NewRequest(&finlakev1.ListDailyExpensesRequest{From: "2025-01-01", To: "2026-12-31"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("ListDailyExpenses over 400 days err = %v", err)
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

func ptr[T any](v T) *T { return &v }
