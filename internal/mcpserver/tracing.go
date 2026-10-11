package mcpserver

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName は span を載せる instrumentation scope。
const tracerName = "github.com/boykush/finlake/internal/mcpserver"

// toolError は、tool の呼び出しに答えたが isError が立っていたときに、OpenTelemetry の MCP の
// conventions が付ける error.type。
const toolError = "tool_error"

// span に残す、リクエストから来た値の上限。span は送るまでメモリで待ち、tool の名前や引数は
// 呼び出し側がリクエストの許す限り長くできる。
const maxFromCaller = 1024

// callerFaults は、送られたままでは答えられないリクエストを断る JSON-RPC のコード。非は呼び出し側に
// あるので、conventions はサーバーのエラーに数えない。最後のものは MCP 独自の、リソースが無いときのもの。
var callerFaults = []int64{
	jsonrpc.CodeParseError,
	jsonrpc.CodeInvalidRequest,
	jsonrpc.CodeMethodNotFound,
	jsonrpc.CodeInvalidParams,
	-32002,
}

// traceContext は、呼び出し側がリクエストの属する trace を渡すやり方。
var traceContext = propagation.TraceContext{}

// traceRequests は、サーバーが受けるメッセージごとに server span を作る。名前と属性は OpenTelemetry の
// MCP の semantic conventions に従う。HTTP ではどのメッセージも同じ POST なので、メソッドと tool を
// 見分けられるのはここだけ。captureContent が立つと、tool の引数とエラーの文面も残す。
func traceRequests(tp trace.TracerProvider, captureContent bool) mcp.Middleware {
	tracer := tp.Tracer(tracerName, trace.WithSchemaURL(semconv.SchemaURL))
	// エラーの文面はリクエストを引用する（月の書式違いなど）ので、引数と同じく opt-in のときだけ残す。
	reason := func(err error) string {
		if err == nil || !captureContent {
			return ""
		}
		return fromCaller(err.Error())
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			// finlake は Streamable HTTP でしか配らない。
			attrs := []attribute.KeyValue{
				semconv.NetworkTransportTCP,
				semconv.NetworkProtocolName("http"),
				semconv.McpMethodNameKey.String(method),
			}
			var tool string
			if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && params != nil {
				tool = params.Name
				attrs = append(attrs, semconv.GenAIOperationNameExecuteTool, semconv.GenAIToolName(fromCaller(tool)))
				// 何も取らない tool の呼び出しは空のオブジェクトを送り、残す価値が無い。
				if arguments := string(params.Arguments); captureContent && arguments != "" && arguments != "{}" {
					attrs = append(attrs, semconv.GenAIToolCallArgumentsKey.String(fromCaller(arguments)))
				}
			}
			ctx, span := tracer.Start(withCallerTrace(ctx, req), method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attrs...),
			)
			defer span.End()

			res, err := next(ctx, method, req)

			if version := protocolVersion(req, res); version != "" {
				span.SetAttributes(semconv.McpProtocolVersion(version))
			}
			if err != nil {
				recordError(span, err, reason)
				return res, err
			}
			// tool の名前を span の名前に足すのは、呼び出しが tool に届いてから。それまでは名前は呼び出し側が
			// 好きに作れ、conventions は上限の無い値を span の名前から外している。
			if tool != "" {
				span.SetName(method + " " + tool)
			}
			if result, ok := res.(*mcp.CallToolResult); ok && result != nil && result.IsError {
				span.SetAttributes(semconv.ErrorTypeKey.String(toolError))
				span.SetStatus(codes.Error, reason(result.GetError()))
			}
			return res, nil
		}
	}
}

// withCallerTrace は、呼び出し側が req を送ったときの trace があれば、それを持たせた ctx を返す。span は
// その trace を続ける。MCP は trace を params._meta で運ぶ。1つの HTTP リクエストのヘッダーは、
// メッセージごとのものにならないため。
func withCallerTrace(ctx context.Context, req mcp.Request) context.Context {
	meta := requestMeta(req)
	carrier := propagation.MapCarrier{}
	for _, field := range traceContext.Fields() {
		if value, ok := meta[field].(string); ok {
			carrier[field] = value
		}
	}
	return traceContext.Extract(ctx, carrier)
}

// requestMeta は params._meta を返す。params 無しで送られたリクエストは、interface の裏に nil の
// ポインタを持って middleware に届き、GetMeta はそれを参照してしまう。
func requestMeta(req mcp.Request) map[string]any {
	params := req.GetParams()
	if params == nil {
		return nil
	}
	if v := reflect.ValueOf(params); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	return params.GetMeta()
}

// protocolVersion は req に使われた MCP の版を返す。ハンドシェイクはその結果で版を決め、後のリクエストは
// 決まった版を運ぶ。
func protocolVersion(req mcp.Request, res mcp.Result) string {
	if init, ok := res.(*mcp.InitializeResult); ok && init != nil {
		return init.ProtocolVersion
	}
	if versioned, ok := req.(interface{ ProtocolVersion() string }); ok {
		return versioned.ProtocolVersion()
	}
	return ""
}

// recordError は err で終わったリクエストの span に印を付ける。JSON-RPC のエラーはコードを持ち、
// どのコードがサーバーの失敗かは conventions が決める。コードの無いエラーはサーバー自身の失敗。
func recordError(span trace.Span, err error, reason func(error) string) {
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		code := strconv.FormatInt(rpcErr.Code, 10)
		span.SetAttributes(semconv.RPCResponseStatusCode(code))
		if slices.Contains(callerFaults, rpcErr.Code) {
			return
		}
		span.SetAttributes(semconv.ErrorTypeKey.String(code))
	} else {
		span.SetAttributes(semconv.ErrorType(err))
	}
	span.SetStatus(codes.Error, reason(err))
}

// fromCaller は呼び出し側の手が入った文字列を、span に残せる形にする。長さを抑え、正しい UTF-8 にする。
// UTF-8 でないバイトが1つあるだけで、span を送るバッチ全体のエンコードが失敗し、他のリクエストの span も
// 道連れになる。
func fromCaller(s string) string {
	if len(s) > maxFromCaller {
		s = s[:maxFromCaller]
	}
	return strings.ToValidUTF8(s, "�")
}
