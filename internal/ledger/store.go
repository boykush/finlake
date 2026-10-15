// Package ledger は product の明細を引き、finlake.v1.LedgerService として配る。
package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/internal/lake"
)

// snapshotTable は product の明細を手元に写した表。R2 を毎回読むと問い合わせ1回に1秒前後かかるので、
// 明細が変わったときだけ読み直し、問い合わせはこの表に当てる。
const snapshotTable = "product_transactions"

// Store は product の明細を DuckDB で引く。
type Store struct {
	lake *lake.Lake
	// checkEvery は明細が変わったかを lake に確かめる間隔。sync は日に1回なので、分単位で十分。
	checkEvery time.Duration

	mu      sync.Mutex
	checked time.Time
	stamp   string
	empty   bool
}

// NewStore は lake の product を引く Store を返す。
func NewStore(l *lake.Lake) *Store { return &Store{lake: l, checkEvery: time.Minute} }

// transactions は問い合わせが読む FROM 句。
func (s *Store) transactions() string { return snapshotTable }

// refresh は前に確かめてから checkEvery が過ぎていれば、product のファイルと更新時刻を見て、変わっていれば
// 表を写し直す。
func (s *Store) refresh(ctx context.Context) (empty bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checked.IsZero() && time.Since(s.checked) < s.checkEvery {
		return s.empty, nil
	}
	files, err := s.lake.Modified(ctx, s.lake.TransactionsGlob())
	if err != nil {
		return false, err
	}
	stamp := stampOf(files)
	if s.checked.IsZero() || stamp != s.stamp {
		if len(files) > 0 {
			q := fmt.Sprintf(
				"CREATE OR REPLACE TABLE %s AS SELECT * FROM read_parquet(%s, hive_partitioning = true, hive_types = {'month': VARCHAR})",
				snapshotTable, lake.Quote(s.lake.TransactionsGlob()),
			)
			if _, err := s.lake.DB.ExecContext(ctx, q); err != nil {
				return false, fmt.Errorf("load the product: %w", err)
			}
		}
		s.stamp, s.empty = stamp, len(files) == 0
	}
	s.checked = time.Now()
	return s.empty, nil
}

func stampOf(files map[string]time.Time) string {
	names := slices.Sorted(maps.Keys(files))
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s@%d\n", n, files[n].UnixNano())
	}
	return b.String()
}

// query は明細がまだ1件も無いときを空の結果として扱う。
func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, bool, error) {
	empty, err := s.refresh(ctx)
	if err != nil || empty {
		return nil, false, err
	}
	rows, err := s.lake.DB.QueryContext(ctx, q, args...)
	if err != nil {
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
