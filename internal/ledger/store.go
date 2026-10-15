// Package ledger は product の明細を引き、finlake.v1.LedgerService として配る。
package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/internal/lake"
)

// Store は product の明細を DuckDB で引く。
type Store struct {
	lake *lake.Lake
}

// NewStore は lake の product を引く Store を返す。
func NewStore(l *lake.Lake) *Store { return &Store{lake: l} }

// transactions は product の明細すべてを指す FROM 句。month は hive パーティションから来る。
func (s *Store) transactions() string {
	return fmt.Sprintf(
		"read_parquet(%s, hive_partitioning = true, hive_types = {'month': VARCHAR})",
		lake.Quote(s.lake.TransactionsGlob()),
	)
}

// query は明細がまだ1件も無いときの「ファイルが無い」エラーを空の結果として扱う。
func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, bool, error) {
	rows, err := s.lake.DB.QueryContext(ctx, q, args...)
	if err != nil {
		if strings.Contains(err.Error(), "No files found") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return rows, true, nil
}

const totalsSelect = `
  coalesce(sum(amount) FILTER (kind = 'income'), 0)::BIGINT,
  coalesce(-sum(amount) FILTER (kind = 'expense'), 0)::BIGINT,
  coalesce(sum(amount) FILTER (kind IN ('income', 'expense')), 0)::BIGINT,
  count(*) FILTER (kind IN ('income', 'expense'))`

// Months は取り込み済みの月と、その収支を新しい順に返す。
func (s *Store) Months(ctx context.Context) ([]*finlakev1.MonthTotal, error) {
	rows, ok, err := s.query(ctx, "SELECT month,"+totalsSelect+" FROM "+s.transactions()+" GROUP BY month ORDER BY month DESC")
	if err != nil || !ok {
		return nil, err
	}
	defer rows.Close()

	var out []*finlakev1.MonthTotal
	for rows.Next() {
		t := &finlakev1.MonthTotal{}
		if err := rows.Scan(&t.Month, &t.Income, &t.Expense, &t.Balance, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Summary は対象月の収支と大項目ごとの内訳を返す。明細が無ければ nil。
func (s *Store) Summary(ctx context.Context, month string) (*finlakev1.GetMonthlySummaryResponse, error) {
	rows, ok, err := s.query(ctx, "SELECT"+totalsSelect+" FROM "+s.transactions()+" WHERE month = ?", month)
	if err != nil || !ok {
		return nil, err
	}
	sum := &finlakev1.GetMonthlySummaryResponse{Month: month}
	for rows.Next() {
		if err := rows.Scan(&sum.Income, &sum.Expense, &sum.Balance, &sum.Count); err != nil {
			_ = rows.Close()
			return nil, err
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if sum.Count == 0 {
		return nil, nil
	}

	rows, _, err = s.query(ctx, `
SELECT kind, coalesce(category, '未分類'), abs(sum(amount))::BIGINT, count(*)
FROM `+s.transactions()+`
WHERE month = ? AND kind IN ('income', 'expense')
GROUP BY ALL
ORDER BY kind DESC, 3 DESC`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		c := &finlakev1.CategoryTotal{}
		if err := rows.Scan(&c.Kind, &c.Category, &c.Amount, &c.Count); err != nil {
			return nil, err
		}
		sum.Categories = append(sum.Categories, c)
	}
	return sum, rows.Err()
}

// TransactionFilter は明細の絞り込み条件。空の項目は条件にしない。
type TransactionFilter struct {
	Month    string
	Kind     string
	Category string
	Keyword  string
	Limit    int32
}

// Transactions は条件に合う明細を日付順に返す。
func (s *Store) Transactions(ctx context.Context, f TransactionFilter) ([]*finlakev1.Transaction, error) {
	where := []string{"month = ?"}
	args := []any{f.Month}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.Keyword != "" {
		where = append(where, "(description ILIKE ? OR memo ILIKE ?)")
		kw := "%" + f.Keyword + "%"
		args = append(args, kw, kw)
	}
	args = append(args, f.Limit)

	rows, ok, err := s.query(ctx, `
SELECT id, date, description, amount, kind, category, subcategory, account, memo
FROM `+s.transactions()+`
WHERE `+strings.Join(where, " AND ")+`
ORDER BY date, id
LIMIT ?`, args...)
	if err != nil || !ok {
		return nil, err
	}
	defer rows.Close()

	var out []*finlakev1.Transaction
	for rows.Next() {
		t := &finlakev1.Transaction{}
		var date time.Time
		if err := rows.Scan(&t.Id, &date, &t.Description, &t.Amount, &t.Kind, &t.Category, &t.Subcategory, &t.Account, &t.Memo); err != nil {
			return nil, err
		}
		t.Date = date.Format(time.DateOnly)
		out = append(out, t)
	}
	return out, rows.Err()
}

// CategoryTrend は大項目（と中項目）の支出または収入を月ごとに返す。from / to は含む。
func (s *Store) CategoryTrend(ctx context.Context, kind, category, subcategory, from, to string) ([]*finlakev1.TrendPoint, error) {
	where := []string{"kind = ?", "category = ?"}
	args := []any{kind, category}
	if subcategory != "" {
		where = append(where, "subcategory = ?")
		args = append(args, subcategory)
	}
	if from != "" {
		where = append(where, "month >= ?")
		args = append(args, from)
	}
	if to != "" {
		where = append(where, "month <= ?")
		args = append(args, to)
	}

	rows, ok, err := s.query(ctx, `
SELECT month, abs(sum(amount))::BIGINT, count(*)
FROM `+s.transactions()+`
WHERE `+strings.Join(where, " AND ")+`
GROUP BY month
ORDER BY month`, args...)
	if err != nil || !ok {
		return nil, err
	}
	defer rows.Close()

	var out []*finlakev1.TrendPoint
	for rows.Next() {
		p := &finlakev1.TrendPoint{}
		if err := rows.Scan(&p.Month, &p.Amount, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
