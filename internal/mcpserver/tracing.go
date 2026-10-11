package mcpserver

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// span の名前と属性は MCP の semantic conventions に合わせる。
// https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/mcp.md

const methodCallTool = "tools/call"

// callerMistakes は、送られたままでは応えられなかったリクエストの JSON-RPC コード。
// conventions はコードを記録するが、エラーには数えない。
var callerMistakes = []int64{
	jsonrpc.CodeParseError,
	jsonrpc.CodeInvalidRequest,
	jsonrpc.CodeMethodNotFound,
	jsonrpc.CodeInvalidParams,
}

func traceVersion(version string) trace.TracerOption {
	return trace.WithInstrumentationVersion(version)
}

// traceRequests はリクエストごとに server span を開く。captureContent が偽なら、tool の引数と
// エラーの文面（引数をそのまま含みうる）は載せない。
func traceRequests(tracer trace.Tracer, captureContent bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			name := method
			attrs := []attribute.KeyValue{attribute.String("mcp.method.name", method)}
			attrs = append(attrs, sessionAttributes(req)...)

			if tool, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && tool != nil && method == methodCallTool {
				name = method + " " + tool.Name
				attrs = append(attrs,
					attribute.String("gen_ai.operation.name", "execute_tool"),
					attribute.String("gen_ai.tool.name", tool.Name),
				)
				if captureContent && len(tool.Arguments) > 0 {
					attrs = append(attrs, attribute.String("gen_ai.tool.call.arguments", string(tool.Arguments)))
				}
			}

			ctx, span := tracer.Start(callerContext(ctx, req), name,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attrs...),
			)
			defer span.End()

			result, err := next(ctx, method, req)
			recordOutcome(span, result, err, captureContent)
			return result, err
		}
	}
}

// sessionAttributes は接続から分かる属性。Streamable HTTP ではトランスポートが HTTP ヘッダを渡してくる。
func sessionAttributes(req mcp.Request) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if session := req.GetSession(); session != nil && session.ID() != "" {
		attrs = append(attrs, attribute.String("mcp.session.id", session.ID()))
	}
	extra := req.GetExtra()
	if extra == nil || extra.Header == nil {
		return append(attrs, attribute.String("network.transport", "pipe"))
	}
	attrs = append(attrs,
		attribute.String("network.transport", "tcp"),
		attribute.String("network.protocol.name", "http"),
	)
	if v := extra.Header.Get("Mcp-Protocol-Version"); v != "" {
		attrs = append(attrs, attribute.String("mcp.protocol.version", v))
	}
	return attrs
}

// callerContext は呼び出し元の trace を親にした context を返す。MCP は W3C Trace Context を
// HTTP ヘッダではなく params._meta で運ぶ（stdio でも通るように）。
func callerContext(ctx context.Context, req mcp.Request) context.Context {
	params := req.GetParams()
	// params を省いたリクエストは型の付いた nil ポインタで届き、そのまま GetMeta を呼ぶと落ちる。
	if v := reflect.ValueOf(params); !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) {
		return ctx
	}
	return propagation.TraceContext{}.Extract(ctx, metaCarrier(params.GetMeta()))
}

type metaCarrier map[string]any

func (c metaCarrier) Get(key string) string {
	s, _ := c[key].(string)
	return s
}

func (c metaCarrier) Set(key, value string) { c[key] = value }

func (c metaCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

func recordOutcome(span trace.Span, result mcp.Result, err error, captureContent bool) {
	fail := func(errorType, message string) {
		span.SetAttributes(attribute.String("error.type", errorType))
		if !captureContent {
			message = ""
		}
		span.SetStatus(codes.Error, message)
	}

	if err != nil {
		var rpcErr *jsonrpc.Error
		if !errors.As(err, &rpcErr) {
			fail("_OTHER", err.Error())
			return
		}
		code := strconv.FormatInt(rpcErr.Code, 10)
		span.SetAttributes(attribute.String("rpc.response.status_code", code))
		if !slices.Contains(callerMistakes, rpcErr.Code) {
			fail(code, rpcErr.Message)
		}
		return
	}

	// tool の失敗は、成功した JSON-RPC レスポンスの中で伝わる。
	if res, ok := result.(*mcp.CallToolResult); ok && res != nil && res.IsError {
		fail("tool_error", toolErrorText(res))
	}
}

func toolErrorText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}
