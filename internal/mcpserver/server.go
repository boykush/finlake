// Package mcpserver は product の明細を MCP で配るサーバー。
package mcpserver

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/trace"

	"github.com/boykush/finlake/gen/finlake/v1/finlakev1connect"
	"github.com/boykush/finlake/internal/ledger"
)

// Config はサーバーのトレースの設定。ゼロ値なら何もトレースしない。
type Config struct {
	// TracerProvider があれば、サーバーが受けるリクエストごとに span を作る。
	TracerProvider trace.TracerProvider
	// CaptureContent が立つと、呼び出し側が送ったもの（tool の引数と、それを引用するエラーの文面）を
	// span に残す。conventions は tool の引数を、求められない限り外している。
	CaptureContent bool
}

// NewServer は LedgerService の RPC を tool として載せた MCP サーバーを返す。
func NewServer(svc *ledger.Service, version string, cfg Config) (*mcp.Server, error) {
	service, err := loadService(finlakev1connect.LedgerServiceName)
	if err != nil {
		return nil, err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "finlake", Version: version}, &mcp.ServerOptions{
		Instructions: comment(service),
	})
	if cfg.TracerProvider != nil {
		s.AddReceivingMiddleware(traceRequests(cfg.TracerProvider, cfg.CaptureContent))
	}

	addTool(s, service, "ListMonths", svc.ListMonths)
	addTool(s, service, "GetMonthlySummary", svc.GetMonthlySummary)
	addTool(s, service, "ListTransactions", svc.ListTransactions)
	addTool(s, service, "GetCategoryTrend", svc.GetCategoryTrend)
	addTool(s, service, "ListDailyExpenses", svc.ListDailyExpenses)
	addTool(s, service, "AggregateTransactions", svc.AggregateTransactions)
	return s, nil
}

// Handler は /mcp と /healthz を持つ HTTP ハンドラを返す。
// 認証は持たない。家計は非公開なので、公開する側（Cloudflare Access）が前段で必ず認証する。
func Handler(server *mcp.Server) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		// セッションを持たないので、Pod が入れ替わっても接続し直さずに済む。
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
