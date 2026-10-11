package mcpserver

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// tracerName は span の instrumentation scope。
const tracerName = "github.com/boykush/finlake/internal/mcpserver"

// GenAI semantic conventions が、利用者の送った中身を載せる属性の opt-in に充てている変数。
const captureContentEnv = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"

// Telemetry は MCP のリクエストごとの span を OTLP/HTTP で送る。
type Telemetry struct {
	provider       *sdktrace.TracerProvider
	version        string
	captureContent bool
}

// TelemetryFromEnv は OTEL_* の環境変数が送り先を指しているときだけ Telemetry を返す。無ければ nil。
// 送り先以外の設定（サービス名、ヘッダ、タイムアウト）も SDK が同じ環境変数から読む。
func TelemetryFromEnv(ctx context.Context, version string) (*Telemetry, error) {
	endpoint := otlpEndpoint(os.Getenv)
	if endpoint == "" {
		return nil, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	// 既定では標準の log に出るので、ほかのログと同じ形に揃える。
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Warn("telemetry", "error", err)
	}))
	slog.Info("exporting traces", "endpoint", endpoint) //nolint:gosec // G706: 設定から来た値で、外部入力ではない

	return &Telemetry{
		provider:       sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter)),
		version:        version,
		captureContent: capturesContent(os.Getenv),
	}, nil
}

// Middleware はリクエストごとに server span を開く受信側の middleware を返す。
func (t *Telemetry) Middleware() mcp.Middleware {
	return traceRequests(t.provider.Tracer(tracerName, traceVersion(t.version)), t.captureContent)
}

// Shutdown は溜まっている span を送り切る。バッチは数秒おきにしか出ないので、止める前に呼ぶ。
func (t *Telemetry) Shutdown(ctx context.Context) error {
	return t.provider.Shutdown(ctx)
}

// otlpEndpoint は exporter が使う送り先。OTLP の規則どおり traces 用の変数が勝ち、空は未設定と同じ。
func otlpEndpoint(getenv func(string) string) string {
	for _, name := range []string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT"} {
		if v := getenv(name); v != "" {
			return v
		}
	}
	return ""
}

func capturesContent(getenv func(string) string) bool {
	return strings.EqualFold(getenv(captureContentEnv), "true")
}
