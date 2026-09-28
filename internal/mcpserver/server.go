// Package mcpserver は product の明細を MCP で配るサーバー。
package mcpserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const instructions = `家計（マネーフォワード ME の入出金明細）を月単位で引く。
金額は円。明細の amount は収入が正、支出が負。集計の income / expense はどちらも正の値。
振替（transfer）と計算対象外（excluded）は収支に入らない。
まず list_months で取り込み済みの月を見てから、月を指定して他の tool を使う。`

var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// NewServer は tool を載せた MCP サーバーを返す。
func NewServer(store *Store, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "finlake", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_months",
		Description: "取り込み済みの月と、月ごとの収入・支出・収支を新しい順に返す",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listMonthsOut, error) {
		months, err := store.Months(ctx)
		return nil, listMonthsOut{Months: months}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_monthly_summary",
		Description: "対象月の収入・支出・収支と、大項目ごとの内訳を返す",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in monthIn) (*mcp.CallToolResult, *Summary, error) {
		if err := checkMonth(in.Month); err != nil {
			return nil, nil, err
		}
		sum, err := store.Summary(ctx, in.Month)
		if err != nil {
			return nil, nil, err
		}
		if sum == nil {
			return nil, nil, fmt.Errorf("no transactions for %s", in.Month)
		}
		return nil, sum, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_transactions",
		Description: "対象月の明細を日付順に返す。区分・大項目・キーワード（内容とメモの部分一致）で絞り込める",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTransactionsIn) (*mcp.CallToolResult, listTransactionsOut, error) {
		if err := checkMonth(in.Month); err != nil {
			return nil, listTransactionsOut{}, err
		}
		limit := in.Limit
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		txs, err := store.Transactions(ctx, TransactionFilter{
			Month: in.Month, Kind: in.Kind, Category: in.Category, Keyword: in.Keyword, Limit: limit,
		})
		return nil, listTransactionsOut{Transactions: txs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_category_trend",
		Description: "大項目（任意で中項目）の支出または収入を月ごとに返す。期間を省くと取り込み済みの全期間",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in categoryTrendIn) (*mcp.CallToolResult, categoryTrendOut, error) {
		kind := in.Kind
		if kind == "" {
			kind = "expense"
		}
		if kind != "expense" && kind != "income" {
			return nil, categoryTrendOut{}, fmt.Errorf("kind must be expense or income")
		}
		for _, m := range []string{in.From, in.To} {
			if m != "" {
				if err := checkMonth(m); err != nil {
					return nil, categoryTrendOut{}, err
				}
			}
		}
		points, err := store.CategoryTrend(ctx, kind, in.Category, in.Subcategory, in.From, in.To)
		return nil, categoryTrendOut{Points: points}, err
	})

	return s
}

type listMonthsOut struct {
	Months []MonthTotal `json:"months"`
}

type monthIn struct {
	Month string `json:"month" jsonschema:"対象月（YYYY-MM）"`
}

type listTransactionsIn struct {
	Month    string `json:"month" jsonschema:"対象月（YYYY-MM）"`
	Kind     string `json:"kind,omitempty" jsonschema:"income / expense / transfer / excluded のどれか"`
	Category string `json:"category,omitempty" jsonschema:"大項目（完全一致）"`
	Keyword  string `json:"keyword,omitempty" jsonschema:"内容とメモの部分一致"`
	Limit    int    `json:"limit,omitempty" jsonschema:"最大件数（既定 200、上限 1000）"`
}

type listTransactionsOut struct {
	Transactions []Transaction `json:"transactions"`
}

type categoryTrendIn struct {
	Category    string `json:"category" jsonschema:"大項目（完全一致）"`
	Subcategory string `json:"subcategory,omitempty" jsonschema:"中項目（完全一致）"`
	Kind        string `json:"kind,omitempty" jsonschema:"expense（既定）か income"`
	From        string `json:"from,omitempty" jsonschema:"開始月（YYYY-MM、含む）"`
	To          string `json:"to,omitempty" jsonschema:"終了月（YYYY-MM、含む）"`
}

type categoryTrendOut struct {
	Points []TrendPoint `json:"points"`
}

func checkMonth(m string) error {
	if !monthPattern.MatchString(m) {
		return fmt.Errorf("month must be YYYY-MM: %q", m)
	}
	return nil
}

// Handler は /mcp（Bearer トークン必須）と /healthz（無認証）を持つ HTTP ハンドラを返す。
// tokens のどれかと一致する Bearer トークンだけを通す。
func Handler(server *mcp.Server, tokens []string) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		// セッションを持たないので、Pod が入れ替わっても接続し直さずに済む。
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.RequireBearerToken(verifier(tokens), nil)(mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// verifier は静的なトークンの一覧と照合する。長さの違いからも漏れないよう、ハッシュ同士を比べる。
func verifier(tokens []string) auth.TokenVerifier {
	hashes := make([][32]byte, 0, len(tokens))
	for _, t := range tokens {
		if t != "" {
			hashes = append(hashes, sha256.Sum256([]byte(t)))
		}
	}
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		got := sha256.Sum256([]byte(token))
		ok := 0
		for _, h := range hashes {
			ok |= subtle.ConstantTimeCompare(got[:], h[:])
		}
		if ok != 1 {
			return nil, auth.ErrInvalidToken
		}
		// 静的トークンに期限は無いが、SDK は期限を必須にしているので先の時刻を入れる。
		return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
	}
}
