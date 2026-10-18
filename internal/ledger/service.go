package ledger

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"connectrpc.com/connect"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/gen/finlake/v1/finlakev1connect"
)

var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// Service は finlake.v1.LedgerService の実装。MCP の tool はこれを呼ぶ。
type Service struct {
	store *Store
}

var _ finlakev1connect.LedgerServiceHandler = (*Service)(nil)

// NewService は store を引く Service を返す。
func NewService(store *Store) *Service { return &Service{store: store} }

func invalid(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(format, args...))
}

func parseDate(name, d string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return time.Time{}, invalid("%s must be YYYY-MM-DD: %q", name, d)
	}
	return t, nil
}

// checkPeriod は from から to（どちらも含む）が正しい期間かを確かめる。
func checkPeriod(from, to string, maxDays int) error {
	f, err := parseDate("from", from)
	if err != nil {
		return err
	}
	t, err := parseDate("to", to)
	if err != nil {
		return err
	}
	if t.Before(f) {
		return invalid("to %s is before from %s", to, from)
	}
	if maxDays > 0 && t.Sub(f) >= time.Duration(maxDays)*24*time.Hour {
		return invalid("the period must be at most %d days", maxDays)
	}
	return nil
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
	switch {
	case in.Month != nil && (in.From != nil || in.To != nil):
		return nil, invalid("give either month or from and to, not both")
	case in.Month != nil:
		if err := checkMonth(in.GetMonth()); err != nil {
			return nil, err
		}
	default:
		if err := checkPeriod(in.GetFrom(), in.GetTo(), 0); err != nil {
			return nil, err
		}
	}
	limit := in.GetLimit()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	txs, err := s.store.Transactions(ctx, TransactionFilter{
		Month: in.GetMonth(), From: in.GetFrom(), To: in.GetTo(), Kind: in.GetKind(), Category: in.GetCategory(), Keyword: in.GetKeyword(), Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&finlakev1.ListTransactionsResponse{Transactions: txs}), nil
}
