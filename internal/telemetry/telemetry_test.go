package telemetry

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/caarlos0/env/v11"
)

const serviceName = "finlake-under-test"

// 変数名は otlpEnv から取らず、デプロイ側が書く綴りで持つ。テストで宣言を名前に縛るため。
const (
	endpointEnv       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	tracesEndpointEnv = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	protocolEnv       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	tracesProtocolEnv = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
)

// SDK はサービス名をプロセスで一度しか読まないので、それを見るテストではなく最初のテストの前に決める。
func TestMain(m *testing.M) {
	if err := os.Setenv("OTEL_SERVICE_NAME", serviceName); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// setEnv はテストが指す OTLP の環境だけをプロセスに与え、テストを流す環境（collector が指されているかも
// しれない）のものは消す。exporter はエンドポイントをプロセスの環境から読み、map は届かないので、送る
// テストで使う。
func setEnv(t *testing.T, environment map[string]string) {
	t.Helper()
	for _, name := range []string{endpointEnv, tracesEndpointEnv, protocolEnv, tracesProtocolEnv} {
		t.Setenv(name, environment[name])
	}
}

// posted は collector が受けた1つのリクエスト。
type posted struct {
	path string
	body []byte
}

// newCollector は OTLP/HTTP の受け口の代わりに立ち、送られたものを持っておく。
func newCollector(t *testing.T) (url string, received <-chan posted) {
	t.Helper()
	requests := make(chan posted, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read the export: %v", err)
		}
		requests <- posted{path: r.URL.Path, body: body}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, requests
}

// exported は shutdown が collector に送ったものを返す。shutdown はもう戻っているので、まだ来ていない
// 送信は shutdown が待たなかったもの。
func exported(t *testing.T, received <-chan posted) posted {
	t.Helper()
	select {
	case got := <-received:
		return got
	default:
		t.Fatal("the shutdown returned with the span unsent")
		return posted{}
	}
}

// TestNoEndpointNoProvider は collector を向けられていないサーバー。手元で流すときは既定の送り先へ
// 送り始めてはいけない。
func TestNoEndpointNoProvider(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		// nil ではなく空の map。nil だとプロセスの環境が読まれる。
		{name: "unset", env: map[string]string{}},
		// 変数があっても空なら、どの collector も指していない。
		{name: "blank", env: map[string]string{endpointEnv: "", tracesEndpointEnv: ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tp, err := newTracerProvider(context.Background(), "test", env.Options{Environment: c.env})
			if err != nil {
				t.Fatalf("NewTracerProvider: %v", err)
			}
			if tp != nil {
				t.Error("got a tracer provider with no endpoint in the environment")
			}
		})
	}
}

// TestSpansReachTheEndpoint は2つの変数それぞれで collector を指し、届いたものを読む。
func TestSpansReachTheEndpoint(t *testing.T) {
	cases := []struct {
		env string
		// path は変数に書く collector のアドレスの後ろに付ける。
		path     string
		wantPath string
	}{
		// 一般の変数は base で、exporter が signal のパスを足す。
		{env: endpointEnv, wantPath: "/v1/traces"},
		// traces の変数はアドレスそのもの。
		{env: tracesEndpointEnv, path: "/traces", wantPath: "/traces"},
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			ctx := context.Background()
			url, received := newCollector(t)
			setEnv(t, map[string]string{c.env: url + c.path})

			tp, err := NewTracerProvider(ctx, "v-under-test")
			if err != nil {
				t.Fatalf("NewTracerProvider: %v", err)
			}
			_, span := tp.Tracer("test").Start(ctx, "span-under-test")
			span.End()

			// span はまとめて送るので、できたばかりのものはまだキューにある。送るのは shutdown。
			select {
			case <-received:
				t.Fatal("the span was exported before the shutdown")
			default:
			}
			if err := tp.Shutdown(ctx); err != nil {
				t.Fatalf("shutdown: %v", err)
			}

			got := exported(t, received)
			if got.path != c.wantPath {
				t.Errorf("exported to %s, want %s", got.path, c.wantPath)
			}
			// protobuf は文字列をそのままのバイト列で運ぶので、デコードせずに探せる。
			for _, want := range []string{serviceName, "v-under-test", "span-under-test"} {
				if !bytes.Contains(got.body, []byte(want)) {
					t.Errorf("the export does not carry %q", want)
				}
			}
		})
	}
}

// TestAnotherProtocolIsRefused は標準の変数で、finlake が送れないプロトコルを求める。
func TestAnotherProtocolIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		refused bool
	}{
		{name: "general", env: map[string]string{protocolEnv: "grpc"}, refused: true},
		{name: "traces", env: map[string]string{tracesProtocolEnv: "grpc"}, refused: true},
		// traces の変数のほうが具体的なので勝つ。
		{name: "traces over general", env: map[string]string{protocolEnv: "grpc", tracesProtocolEnv: "http/protobuf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			c.env[endpointEnv] = "http://collector.invalid:4317"

			tp, err := newTracerProvider(ctx, "test", env.Options{Environment: c.env})
			if !c.refused {
				if err != nil {
					t.Fatalf("NewTracerProvider: %v", err)
				}
				if err := tp.Shutdown(ctx); err != nil {
					t.Fatalf("shutdown: %v", err)
				}
				return
			}
			if tp != nil {
				t.Error("got a tracer provider for a protocol finlake does not export over")
			}
			// 変数を設定した人には、どの値を断ったかが要る。
			if err == nil || !strings.Contains(err.Error(), `"grpc"`) {
				t.Errorf("err = %v, want it to name the protocol", err)
			}
		})
	}
}
