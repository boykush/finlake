package mcpserver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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

// MonthTotal は月ごとの収支。expense は支出の絶対値。
type MonthTotal struct {
	Month   string `json:"month" jsonschema:"対象月（YYYY-MM）"`
	Income  int64  `json:"income" jsonschema:"収入の合計（円）"`
	Expense int64  `json:"expense" jsonschema:"支出の合計（円、正の値）"`
	Balance int64  `json:"balance" jsonschema:"収入 - 支出（円）"`
	Count   int64  `json:"count" jsonschema:"収支に入る明細の件数"`
}

const totalsSelect = `
  coalesce(sum(amount) FILTER (kind = 'income'), 0)::BIGINT,
  coalesce(-sum(amount) FILTER (kind = 'expense'), 0)::BIGINT,
  coalesce(sum(amount) FILTER (kind IN ('income', 'expense')), 0)::BIGINT,
  count(*) FILTER (kind IN ('income', 'expense'))`

// Months は取り込み済みの月と、その収支を新しい順に返す。
func (s *Store) Months(ctx context.Context) ([]MonthTotal, error) {
	rows, ok, err := s.query(ctx, "SELECT month,"+totalsSelect+" FROM "+s.transactions()+" GROUP BY month ORDER BY month DESC")
	if err != nil || !ok {
		return []MonthTotal{}, err
	}
	defer rows.Close()

	out := []MonthTotal{}
	for rows.Next() {
		var t MonthTotal
		if err := rows.Scan(&t.Month, &t.Income, &t.Expense, &t.Balance, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CategoryTotal は大項目ごとの合計。amount は絶対値。
type CategoryTotal struct {
	Kind     string `json:"kind" jsonschema:"income か expense"`
	Category string `json:"category" jsonschema:"大項目"`
	Amount   int64  `json:"amount" jsonschema:"合計（円、正の値）"`
	Count    int64  `json:"count" jsonschema:"件数"`
}

// Summary は月の収支と大項目ごとの内訳。
type Summary struct {
	MonthTotal
	Categories []CategoryTotal `json:"categories" jsonschema:"大項目ごとの内訳。金額の大きい順"`
}

// Summary は対象月の収支と大項目ごとの内訳を返す。明細が無ければ nil。
func (s *Store) Summary(ctx context.Context, month string) (*Summary, error) {
	rows, ok, err := s.query(ctx, "SELECT"+totalsSelect+" FROM "+s.transactions()+" WHERE month = ?", month)
	if err != nil || !ok {
		return nil, err
	}
	sum := &Summary{MonthTotal: MonthTotal{Month: month}, Categories: []CategoryTotal{}}
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
		var c CategoryTotal
		if err := rows.Scan(&c.Kind, &c.Category, &c.Amount, &c.Count); err != nil {
			return nil, err
		}
		sum.Categories = append(sum.Categories, c)
	}
	return sum, rows.Err()
}

// Transaction は明細1件。
type Transaction struct {
	ID          string  `json:"id"`
	Date        string  `json:"date" jsonschema:"日付（YYYY-MM-DD）"`
	Description string  `json:"description" jsonschema:"内容"`
	Amount      int64   `json:"amount" jsonschema:"金額（円）。収入が正、支出が負"`
	Kind        string  `json:"kind" jsonschema:"income / expense / transfer（振替）/ excluded（計算対象外）"`
	Category    *string `json:"category,omitempty" jsonschema:"大項目"`
	Subcategory *string `json:"subcategory,omitempty" jsonschema:"中項目"`
	Account     *string `json:"account,omitempty" jsonschema:"保有金融機関"`
	Memo        *string `json:"memo,omitempty" jsonschema:"メモ"`
}

// TransactionFilter は明細の絞り込み条件。空の項目は条件にしない。
type TransactionFilter struct {
	Month    string
	Kind     string
	Category string
	Keyword  string
	Limit    int
}

// Transactions は条件に合う明細を日付順に返す。
func (s *Store) Transactions(ctx context.Context, f TransactionFilter) ([]Transaction, error) {
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
		return []Transaction{}, err
	}
	defer rows.Close()

	out := []Transaction{}
	for rows.Next() {
		var t Transaction
		var date time.Time
		if err := rows.Scan(&t.ID, &date, &t.Description, &t.Amount, &t.Kind, &t.Category, &t.Subcategory, &t.Account, &t.Memo); err != nil {
			return nil, err
		}
		t.Date = date.Format(time.DateOnly)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrendPoint は月ごとの大項目の合計。
type TrendPoint struct {
	Month  string `json:"month"`
	Amount int64  `json:"amount" jsonschema:"合計（円、正の値）"`
	Count  int64  `json:"count"`
}

// CategoryTrend は大項目（と中項目）の支出または収入を月ごとに返す。from / to は含む。
func (s *Store) CategoryTrend(ctx context.Context, kind, category, subcategory, from, to string) ([]TrendPoint, error) {
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
		return []TrendPoint{}, err
	}
	defer rows.Close()

	out := []TrendPoint{}
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.Month, &p.Amount, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
