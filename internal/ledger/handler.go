package ledger

import (
	"net/http"

	"github.com/boykush/finlake/gen/finlake/v1/finlakev1connect"
)

// Handler は LedgerService と /healthz を持つ HTTP ハンドラを返す。
// 認証は持たない。家計は非公開なので、クラスタの外には出さず、呼べる Pod を経路で絞る。
func Handler(svc *Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(finlakev1connect.NewLedgerServiceHandler(svc))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
