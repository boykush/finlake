package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newTracer は span をテストが読める所に残す provider を返す。
func newTracer(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	// テストを流す環境で sampler が指されていると、読める span がそれで決まってしまう。
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, recorder
}

// newTracedServer はトレースするサーバーを HTTP で立てる。
func newTracedServer(t *testing.T, captureContent bool) (url string, recorder *tracetest.SpanRecorder) {
	t.Helper()
	tp, recorder := newTracer(t)
	srv := httptest.NewServer(Handler(NewServer(setup(t), "test", Config{TracerProvider: tp, CaptureContent: captureContent})))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp", recorder
}

// connectHTTP はクライアントとしてサーバーにつなぐ。
func connectHTTP(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func spanAttributes(span sdktrace.ReadOnlySpan) map[attribute.Key]string {
	attrs := make(map[attribute.Key]string)
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value.String()
	}
	return attrs
}

// lastSpan はテストが直前にしたリクエストの span を返す。
func lastSpan(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("no span was reported")
	}
	return spans[len(spans)-1]
}

// TestRequestsAreTraced は、どの呼び出しも同じ POST になる HTTP でサーバーを呼び、呼び出しを
// 見分ける span を読む。
func TestRequestsAreTraced(t *testing.T) {
	ctx := context.Background()
	url, recorder := newTracedServer(t, false)
	cs := connectHTTP(t, url)
	// ハンドシェイクは SDK の形なので、読むのはその後の span。
	handshake := len(recorder.Ended())

	if _, err := cs.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_months", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("call list_months: %v", err)
	}
	if res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_monthly_summary", Arguments: map[string]any{"month": "2020-01"}}); err != nil || !res.IsError {
		t.Fatalf("get_monthly_summary of an empty month: err = %v, want a result that is an error", err)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_balance", Arguments: map[string]any{}}); err == nil {
		t.Fatal("call of a tool the server does not have succeeded")
	}

	toolCall := func(tool string, more map[attribute.Key]string) map[attribute.Key]string {
		attrs := map[attribute.Key]string{"mcp.method.name": "tools/call", "gen_ai.operation.name": "execute_tool", "gen_ai.tool.name": tool}
		maps.Copy(attrs, more)
		return attrs
	}
	want := []struct {
		name   string
		attrs  map[attribute.Key]string
		status codes.Code
	}{
		{name: "tools/list", attrs: map[attribute.Key]string{"mcp.method.name": "tools/list"}},
		{name: "tools/call list_months", attrs: toolCall("list_months", nil)},
		// tool は答え、その答えが失敗だった。送ったものを残すよう求められていないので、span には
		// 呼び出しの月も、それを引用するエラーも残らない。
		{name: "tools/call get_monthly_summary", attrs: toolCall("get_monthly_summary", map[attribute.Key]string{"error.type": "tool_error"}), status: codes.Error},
		// どの tool にも届いていないので名前に tool は入らない。呼び出し側はどんな名前でも求められる。
		// 無いものを求めたのは呼び出し側の非で、サーバーの失敗ではない。
		{name: "tools/call", attrs: toolCall("get_balance", map[attribute.Key]string{"rpc.response.status_code": "-32602"})},
	}

	spans := recorder.Ended()[handshake:]
	// リクエストごとに1つで、それ以外は無い。
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d", len(spans), len(want))
	}
	for i, w := range want {
		span := spans[i]
		if span.Name() != w.name {
			t.Errorf("span %d is named %q, want %q", i, span.Name(), w.name)
		}
		if span.SpanKind() != trace.SpanKindServer {
			t.Errorf("%s: kind = %v, want server", w.name, span.SpanKind())
		}
		// 呼び出し側が trace を送っていないので、それぞれが自分の trace を始める。
		if span.Parent().IsValid() {
			t.Errorf("%s: has a parent, %v", w.name, span.Parent())
		}
		if got := span.Status(); got.Code != w.status || got.Description != "" {
			t.Errorf("%s: status = %v %q, want %v and no description", w.name, got.Code, got.Description, w.status)
		}
		w.attrs["network.transport"] = "tcp"
		w.attrs["network.protocol.name"] = "http"
		w.attrs["mcp.protocol.version"] = cs.InitializeResult().ProtocolVersion
		// 丸ごと比べる。conventions が span から外す属性（tool に届かない呼び出しの operation など）が
		// あれば、ここで余分として出る。
		if got := spanAttributes(span); !maps.Equal(got, w.attrs) {
			t.Errorf("%s: attributes = %v, want %v", w.name, got, w.attrs)
		}
	}
}

// TestHandshakeIsTraced は initialize で始めるクライアントが送るものを送る。一覧は params 無しで
// 来るが、トレースはそれも読めなければならない。
func TestHandshakeIsTraced(t *testing.T) {
	url, recorder := newTracedServer(t, false)

	postMessage(t, url, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"0"}}}`)
	postMessage(t, url, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	spans := recorder.Ended()
	want := []string{"initialize", "tools/list"}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d", len(spans), len(want))
	}
	for i, name := range want {
		if spans[i].Name() != name {
			t.Errorf("span %d is named %q, want %q", i, spans[i].Name(), name)
		}
	}
	// 版はハンドシェイクが決めたもので、それを言うのはその結果だけ。
	if got := spanAttributes(spans[0])["mcp.protocol.version"]; got != "2025-06-18" {
		t.Errorf("initialize: mcp.protocol.version = %q, want 2025-06-18", got)
	}
}

// TestSpanContinuesTheCallersTrace は、MCP が trace context を運ぶ場所（リクエストの _meta）で送る。
func TestSpanContinuesTheCallersTrace(t *testing.T) {
	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)
	url, recorder := newTracedServer(t, false)
	cs := connectHTTP(t, url)
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Meta:      mcp.Meta{"traceparent": "00-" + traceID + "-" + spanID + "-01"},
		Name:      "list_months",
		Arguments: map[string]any{},
	}); err != nil {
		t.Fatalf("call list_months: %v", err)
	}

	span := lastSpan(t, recorder)
	if got := span.SpanContext().TraceID().String(); got != traceID {
		t.Errorf("trace id = %s, want the caller's %s", got, traceID)
	}
	if parent := span.Parent(); parent.SpanID().String() != spanID || !parent.IsRemote() {
		t.Errorf("parent = %v, want the caller's span %s", parent, spanID)
	}
}

// kept は span に残る content を返す。送ったものを残すよう求められたサーバーなら丸ごと、そうでなければ何も。
func kept(capture bool, content string) string {
	if capture {
		return content
	}
	return ""
}

// TestWhatTheCallerSendsIsKeptOnlyOnRequest は、エラーで答える引数で tool を呼ぶ。呼び出し側のものが
// 最も多く span に残る場合。
func TestWhatTheCallerSendsIsKeptOnlyOnRequest(t *testing.T) {
	ctx := context.Background()
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", capture), func(t *testing.T) {
			url, recorder := newTracedServer(t, capture)
			cs := connectHTTP(t, url)

			if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_months", Arguments: map[string]any{}}); err != nil {
				t.Fatalf("call list_months: %v", err)
			}
			// 引数を取らない tool なので、求められても残す引数は無い。
			if arguments, ok := spanAttributes(lastSpan(t, recorder))["gen_ai.tool.call.arguments"]; ok {
				t.Errorf("list_months: gen_ai.tool.call.arguments = %s, want none", arguments)
			}

			if res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_monthly_summary", Arguments: map[string]any{"month": "2020-01"}}); err != nil || !res.IsError {
				t.Fatalf("get_monthly_summary of an empty month: err = %v, want a result that is an error", err)
			}
			span := lastSpan(t, recorder)
			attrs := spanAttributes(span)
			if got, want := attrs["gen_ai.tool.call.arguments"], kept(capture, `{"month":"2020-01"}`); got != want {
				t.Errorf("gen_ai.tool.call.arguments = %q, want %q", got, want)
			}
			// エラーは求められた月を引用するので、status の文面も引数と同じく呼び出し側のもの。
			if got, want := span.Status().Description, kept(capture, "no transactions for 2020-01"); got != want {
				t.Errorf("status description = %q, want %q", got, want)
			}
			// 呼び出しが失敗したことは、呼び出し側が伏せられるものではない。
			if got := span.Status().Code; got != codes.Error || attrs["error.type"] != "tool_error" {
				t.Errorf("status = %v, error.type = %q, want an error of the tool's", got, attrs["error.type"])
			}
		})
	}
}

// TestWhyTheServerFailedIsKeptOnlyOnRequest は tool の答えではないエラーを扱う。これは別の経路で span に
// 届く。finlake をそう失敗させるリクエストは無いので、失敗するハンドラを代わりに置く。
func TestWhyTheServerFailedIsKeptOnlyOnRequest(t *testing.T) {
	failure := &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: `no way to answer "2020-01"`}
	failing := func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, failure }
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", capture), func(t *testing.T) {
			tp, recorder := newTracer(t)
			request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "get_monthly_summary"}}
			if _, err := traceRequests(tp, capture)(failing)(context.Background(), "tools/call", request); !errors.Is(err, failure) {
				t.Fatalf("err = %v, want the handler's own", err)
			}

			span := lastSpan(t, recorder)
			if got, want := span.Status().Description, kept(capture, failure.Message); got != want {
				t.Errorf("status description = %q, want %q", got, want)
			}
			if got, errorType := span.Status().Code, spanAttributes(span)["error.type"]; got != codes.Error || errorType != "-32603" {
				t.Errorf("status = %v, error.type = %q, want an error of the server's", got, errorType)
			}
		})
	}
}

// postMessage はセッションを持たないクライアントのように、JSON-RPC のメッセージを1つずつ POST で送る。
// params を marshal するのではなくバイト列で取るので、行儀の良いクライアントが送らないものも送れる。
func postMessage(t *testing.T, url, message string) {
	t.Helper()
	res, _ := post(t, url, message)
	if res.StatusCode >= http.StatusBadRequest {
		t.Fatalf("post: %s", res.Status)
	}
}

// TestWhatTheCallerSendsIsKeptInBounds は、span がリクエストを読むすべての場所に、span に収めるには
// 長すぎるものと UTF-8 でないバイトを送る。そうしたバイトが1つあるだけで、同じバッチの span がすべて
// 送れなくなる。
func TestWhatTheCallerSendsIsKeptInBounds(t *testing.T) {
	const notUTF8 = "\xff"
	long := strings.Repeat("9", 4*maxFromCaller)

	for _, message := range []string{
		// サーバーが持たない tool の名前と、引数。
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + long + `","arguments":{"note":"` + notUTF8 + long + `"}}}`,
		// tool のエラーが引用し返す月。
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_monthly_summary","arguments":{"month":"` + notUTF8 + long + `"}}}`,
	} {
		// 呼び出し側が送ったものをすべて残すとき、span が抑えるべきものが最も多い。
		url, recorder := newTracedServer(t, true)
		postMessage(t, url, message)
		span := lastSpan(t, recorder)

		kept := []string{span.Status().Description}
		for _, kv := range span.Attributes() {
			kept = append(kept, kv.Value.String())
		}
		for _, value := range kept {
			// UTF-8 でないバイトの代わりに入る文字は、そのバイトより長い。
			if len(value) > maxFromCaller+utf8.UTFMax {
				t.Errorf("%s: the span keeps %d bytes of one value, want about %d", span.Name(), len(value), maxFromCaller)
			}
			if !utf8.ValidString(value) {
				t.Errorf("%s: the span keeps a value that is not UTF-8: %q", span.Name(), value)
			}
		}
	}
}
