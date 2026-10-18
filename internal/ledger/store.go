// Package ledger は product の明細を引き、finlake.v1.LedgerService として配る。
package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	finlakev1 "github.com/boykush/finlake/gen/finlake/v1"
	"github.com/boykush/finlake/internal/lake"
)

// snapshotTable は product の明細を手元に写した表。R2 を毎回読むと問い合わせ1回に1秒前後かかるので、
// 起動時に写し、明細が変わったら写し直す。問い合わせはこの表に当てる。
const snapshotTable = "product_transactions"

// Store は product の明細を DuckDB で引く。
type Store struct {
	lake *lake.Lake

	mu     sync.Mutex
	loaded bool
	stamp  string
	empty  bool
}

// NewStore は lake の product を引く Store を返す。写すのは Refresh か、最初の問い合わせのとき。
func NewStore(l *lake.Lake) *Store { return &Store{lake: l} }

// transactions は問い合わせが読む FROM 句。
func (s *Store) transactions() string { return snapshotTable }

// Refresh は product のファイルと更新時刻を見て、前に写したときから変わっていれば写し直す。
func (s *Store) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh(ctx)
}

func (s *Store) refresh(ctx context.Context) error {
	files, err := s.lake.Modified(ctx, s.lake.TransactionsGlob())
	if err != nil {
		return err
	}
	stamp := stampOf(files)
	if s.loaded && stamp == s.stamp {
		return nil
	}
	if len(files) > 0 {
		q := fmt.Sprintf(
			"CREATE OR REPLACE TABLE %s AS SELECT * FROM read_parquet(%s, hive_partitioning = true, hive_types = {'month': VARCHAR})",
			snapshotTable, lake.Quote(s.lake.TransactionsGlob()),
		)
		if _, err := s.lake.DB.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("load the product: %w", err)
		}
	}
	s.loaded, s.stamp, s.empty = true, stamp, len(files) == 0
	return nil
}

// Watch は ctx が終わるまで every ごとに Refresh する。sync が product を書いた後に追いつくためのもので、
// リクエストはこれを待たない。
func (s *Store) Watch(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				slog.Warn("refresh the product", "error", err)
			}
		}
	}
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
	s.mu.Lock()
	var err error
	if !s.loaded {
		err = s.refresh(ctx)
	}
	empty := s.empty
	s.mu.Unlock()
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
	From, To string
	Kind     string
	Category string
	Keyword  string
	Limit    int32
}

// Transactions は条件に合う明細を日付順に返す。
func (s *Store) Transactions(ctx context.Context, f TransactionFilter) ([]*finlakev1.Transaction, error) {
	var where []string
	var args []any
	if f.Month != "" {
		where = append(where, "month = ?")
		args = append(args, f.Month)
	}
	if f.From != "" {
		where = append(where, "date BETWEEN ?::DATE AND ?::DATE")
		args = append(args, f.From, f.To)
	}
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
