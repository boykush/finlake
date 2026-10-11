// Package telemetry は finlake のトレースを OTLP で送る。設定は OpenTelemetry の標準の環境変数だけで、
// finlake 独自のものは持たない。デプロイ側は他のサービスと同じやり方で collector を向けられる。
package telemetry

import (
	"context"
	"fmt"

	"github.com/caarlos0/env/v11"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// otlpEnv は標準の変数のうち finlake が自分で読むもの。残りは exporter と resource がそれぞれ読む。
type otlpEnv struct {
	// エンドポイントは exporter が自分で読む。ここで読むのは指定があるかを見るためだけ。exporter は
	// 指定が無いと localhost に向かうが、collector を向けられていないサーバーが送り先を探しに行ってはいけない。
	Endpoint       string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	TracesEndpoint string `env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"`

	// 既定は OTLP の仕様が既定とするプロトコル。
	Protocol       string `env:"OTEL_EXPORTER_OTLP_PROTOCOL" envDefault:"http/protobuf"`
	TracesProtocol string `env:"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"`
}

// httpProtobuf は finlake が持つ exporter のプロトコル。
const httpProtobuf = "http/protobuf"

// NewTracerProvider は環境変数が指す OTLP のエンドポイントへ span を送る provider を返す。指定が無ければ nil。
// version は finlake のビルドで、サービスの版として載せる。span はまとめて送るので、キューに残った分は
// Shutdown が送る。
func NewTracerProvider(ctx context.Context, version string) (*sdktrace.TracerProvider, error) {
	return newTracerProvider(ctx, version, env.Options{})
}

// newTracerProvider は opts が指す環境（指定が無ければプロセスの環境）から otlpEnv を読む。テストが map を
// 渡してプロセスの環境に触れずに済むように分けてある。opts が何を指しても、exporter と resource はプロセスの
// 環境を読む。
func newTracerProvider(ctx context.Context, version string, opts env.Options) (*sdktrace.TracerProvider, error) {
	otlp, err := env.ParseAsWithOptions[otlpEnv](opts)
	if err != nil {
		return nil, fmt.Errorf("read the environment: %w", err)
	}
	if otlp.Endpoint == "" && otlp.TracesEndpoint == "" {
		return nil, nil
	}
	// 別のプロトコル向けのエンドポイントに送ると、起動時に何も言わずに span をすべて失う。
	if p := otlp.protocol(); p != httpProtobuf {
		return nil, fmt.Errorf("OTLP protocol %q is not supported: finlake exports traces over %s", p, httpProtobuf)
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create the OTLP trace exporter: %w", err)
	}
	// 既定の resource は OTEL_SERVICE_NAME からサービス名を取る。版も環境変数が最後に決められるよう、後に重ねる。
	res, err := resource.Merge(resource.NewSchemaless(semconv.ServiceVersion(version)), resource.Default())
	if err != nil {
		return nil, fmt.Errorf("describe the service: %w", err)
	}
	return sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res)), nil
}

// protocol は環境変数が求める OTLP のプロトコルを返す。仕様の定めどおり traces の変数が一般の変数に勝つ。
func (e otlpEnv) protocol() string {
	if e.TracesProtocol != "" {
		return e.TracesProtocol
	}
	return e.Protocol
}
