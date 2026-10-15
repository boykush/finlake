package ledger

import (
	"context"
	"fmt"
	"regexp"

	"connectrpc.com/connect"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/gen/finlake/v1/finlakev1connect"
)

var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// Service は finlake.v1.LedgerService の実装。Connect の API も MCP の tool もこれを呼ぶ。
type Service struct {
	store *Store
}

var _ finlakev1connect.LedgerServiceHandler = (*Service)(nil)

// NewService は store を引く Service を返す。
func NewService(store *Store) *Service { return &Service{store: store} }

func invalid(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(format, args...))
}

func checkMonth(m string) error {
	if !monthPattern.MatchString(m) {
		return invalid("month must be YYYY-MM: %q", m)
	}
	return nil
}

func (s *Service) ListMonths(ctx context.Context, _ *connect.Request[finlakev1.ListMonthsRequest]) (*connect.Response[finlakev1.ListMonthsResponse], error) {
	months, err := s.store.Months(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&finlakev1.ListMonthsResponse{Months: months}), nil
}

func (s *Service) GetMonthlySummary(ctx context.Context, req *connect.Request[finlakev1.GetMonthlySummaryRequest]) (*connect.Response[finlakev1.GetMonthlySummaryResponse], error) {
	month := req.Msg.GetMonth()
	if err := checkMonth(month); err != nil {
		return nil, err
	}
	sum, err := s.store.Summary(ctx, month)
	if err != nil {
		return nil, err
	}
	if sum == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no transactions for %s", month))
	}
	return connect.NewResponse(sum), nil
}

func (s *Service) ListTransactions(ctx context.Context, req *connect.Request[finlakev1.ListTransactionsRequest]) (*connect.Response[finlakev1.ListTransactionsResponse], error) {
	in := req.Msg
	if err := checkMonth(in.GetMonth()); err != nil {
		return nil, err
	}
	limit := in.GetLimit()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	txs, err := s.store.Transactions(ctx, TransactionFilter{
		Month: in.GetMonth(), Kind: in.GetKind(), Category: in.GetCategory(), Keyword: in.GetKeyword(), Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&finlakev1.ListTransactionsResponse{Transactions: txs}), nil
}

func (s *Service) GetCategoryTrend(ctx context.Context, req *connect.Request[finlakev1.GetCategoryTrendRequest]) (*connect.Response[finlakev1.GetCategoryTrendResponse], error) {
	in := req.Msg
	if in.GetCategory() == "" {
		return nil, invalid("category is required")
	}
	kind := in.GetKind()
	if kind == "" {
		kind = "expense"
	}
	if kind != "expense" && kind != "income" {
		return nil, invalid("kind must be expense or income")
	}
	for _, m := range []string{in.GetFrom(), in.GetTo()} {
		if m != "" {
			if err := checkMonth(m); err != nil {
				return nil, err
			}
		}
	}
	points, err := s.store.CategoryTrend(ctx, kind, in.GetCategory(), in.GetSubcategory(), in.GetFrom(), in.GetTo())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&finlakev1.GetCategoryTrendResponse{Points: points}), nil
}
