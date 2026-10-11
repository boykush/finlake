package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// tracedServer は span をメモリに溜める MCP サーバーを立てる。span は閉じた時点で読める。
func tracedServer(t *testing.T, captureContent bool) (string, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	telemetry := &Telemetry{
		provider:       sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)),
		version:        "test",
		captureContent: captureContent,
	}
	server := NewServer(setup(t), "test")
	server.AddReceivingMiddleware(telemetry.Middleware())
	srv := httptest.NewServer(Handler(server))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp", exporter
}

func spanNamed(t *testing.T, exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	t.Helper()
	spans := exporter.GetSpans()
	for _, s := range spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no span named %q in %d spans", name, len(spans))
	return tracetest.SpanStub{}
}

func attr(s tracetest.SpanStub, key string) string {
	for _, a := range s.Attributes {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func TestToolCallSpan(t *testing.T) {
	url, exporter := tracedServer(t, false)

	// MCP は呼び出し元の trace を HTTP ヘッダではなく params._meta で運ぶ。
	resp, body := post(t, url, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{
		"name":"get_monthly_summary","arguments":{"month":"2026-09"},
		"_meta":{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	span := spanNamed(t, exporter, "tools/call get_monthly_summary")
	if span.SpanKind != trace.SpanKindServer {
		t.Errorf("kind = %v", span.SpanKind)
	}
	if got := span.SpanContext.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %s", got)
	}
	if got := span.Parent.SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("parent span id = %s", got)
	}
	for key, want := range map[string]string{
		"mcp.method.name":            "tools/call",
		"gen_ai.operation.name":      "execute_tool",
		"gen_ai.tool.name":           "get_monthly_summary",
		"network.transport":          "tcp",
		"network.protocol.name":      "http",
		"gen_ai.tool.call.arguments": "",
		"error.type":                 "",
	} {
		if got := attr(span, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if span.Status.Code != codes.Unset {
		t.Errorf("status = %+v", span.Status)
	}
}

func TestToolErrorSpan(t *testing.T) {
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_monthly_summary","arguments":{"month":"2026-9"}}}`

	for _, capture := range []bool{false, true} {
		url, exporter := tracedServer(t, capture)
		if _, body := post(t, url, call); !strings.Contains(body, `"isError":true`) {
			t.Fatalf("body = %s", body)
		}

		span := spanNamed(t, exporter, "tools/call get_monthly_summary")
		if got := attr(span, "error.type"); got != "tool_error" {
			t.Errorf("capture=%v: error.type = %q", capture, got)
		}
		if span.Status.Code != codes.Error {
			t.Errorf("capture=%v: status = %+v", capture, span.Status)
		}
		// 引数とエラーの文面は、opt-in したときだけ span に載る。
		if got := attr(span, "gen_ai.tool.call.arguments") != ""; got != capture {
			t.Errorf("capture=%v: arguments recorded = %v", capture, got)
		}
		if got := strings.Contains(span.Status.Description, "2026-9"); got != capture {
			t.Errorf("capture=%v: status description = %q", capture, span.Status.Description)
		}
	}
}

func TestProtocolErrorSpans(t *testing.T) {
	url, exporter := tracedServer(t, false)

	// params の無いリクエストでも落ちない。
	if resp, body := post(t, url, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	spanNamed(t, exporter, "ping")

	// 存在しない tool は呼び出し側の間違いで、コードは残すがエラーには数えない。
	post(t, url, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	span := spanNamed(t, exporter, "tools/call nope")
	if got := attr(span, "rpc.response.status_code"); got != "-32602" {
		t.Errorf("rpc.response.status_code = %q", got)
	}
	if span.Status.Code != codes.Unset || attr(span, "error.type") != "" {
		t.Errorf("status = %+v, error.type = %q", span.Status, attr(span, "error.type"))
	}
}

func TestTelemetryEnv(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	general, traces := "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"

	for _, c := range []struct {
		vars map[string]string
		want string
	}{
		{nil, ""},
		{map[string]string{general: ""}, ""},
		{map[string]string{general: "http://collector:4318"}, "http://collector:4318"},
		{map[string]string{general: "http://collector:4318", traces: "http://traces:4318/v1/traces"}, "http://traces:4318/v1/traces"},
	} {
		if got := otlpEndpoint(env(c.vars)); got != c.want {
			t.Errorf("otlpEndpoint(%v) = %q, want %q", c.vars, got, c.want)
		}
	}

	for value, want := range map[string]bool{"": false, "true": true, "TRUE": true, "false": false, "1": false} {
		if got := capturesContent(env(map[string]string{captureContentEnv: value})); got != want {
			t.Errorf("capturesContent(%q) = %v", value, got)
		}
	}
}
