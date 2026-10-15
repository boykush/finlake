package mcpserver

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// descriptorSet は proto をコメントごと固めたもの（mise run gen が作る）。Go の生成コードは
// コメントを持たないので、tool の説明はここから読む。
//
//go:embed descriptor.binpb
var descriptorSet []byte

// loadService は埋め込んだ記述子から name の service を引く。
func loadService(name string) (protoreflect.ServiceDescriptor, error) {
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(descriptorSet, &set); err != nil {
		return nil, fmt.Errorf("read the embedded descriptors: %w", err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("read the embedded descriptors: %w", err)
	}
	return findService(files, name)
}

func findService(files *protoregistry.Files, name string) (protoreflect.ServiceDescriptor, error) {
	d, err := files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, err
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not a service", name)
	}
	return sd, nil
}

// comment は d の直前に書かれたコメントを返す。
func comment(d protoreflect.Descriptor) string {
	return strings.TrimSpace(d.ParentFile().SourceLocations().ByDescriptor(d).LeadingComments)
}

// addTool は service の RPC を1つ、tool として載せる。名前・説明・入出力の形は proto から取る。
func addTool[Req, Res any, PReq interface {
	*Req
	proto.Message
}, PRes interface {
	*Res
	proto.Message
}](s *mcp.Server, service protoreflect.ServiceDescriptor, method string, call func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error)) {
	md := service.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		panic(fmt.Sprintf("%s has no method %s", service.FullName(), method))
	}
	tool := &mcp.Tool{
		Name:         snakeCase(method),
		Description:  comment(md),
		InputSchema:  messageSchema(md.Input()),
		OutputSchema: messageSchema(md.Output()),
	}
	if opts, ok := md.Options().(*descriptorpb.MethodOptions); ok && opts.GetIdempotencyLevel() == descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	}

	s.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result := &mcp.CallToolResult{}
		in := PReq(new(Req))
		if args := req.Params.Arguments; len(args) > 0 {
			if err := protojson.Unmarshal(args, in); err != nil {
				result.SetError(fmt.Errorf("invalid arguments: %w", err))
				return result, nil
			}
		}
		res, err := call(ctx, connect.NewRequest((*Req)(in)))
		if err != nil {
			result.SetError(unwrapConnect(err))
			return result, nil
		}
		out := messageValue(PRes(res.Msg).ProtoReflect())
		text, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		result.StructuredContent = out
		result.Content = []mcp.Content{&mcp.TextContent{Text: string(text)}}
		return result, nil
	})
}

// unwrapConnect は Connect のコード（"not_found: " など）を文面から外す。tool の利用側が読むのは理由だけ。
func unwrapConnect(err error) error {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return errors.New(ce.Message())
	}
	return err
}

func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// messageSchema は message の JSON Schema を返す。optional の付かない項目は必須にする。
func messageSchema(md protoreflect.MessageDescriptor) map[string]any {
	props := map[string]any{}
	required := []string{}
	fields := md.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		schema := fieldSchema(fd)
		if fd.IsList() {
			schema = map[string]any{"type": "array", "items": schema}
		} else if !fd.HasOptionalKeyword() {
			required = append(required, string(fd.Name()))
		}
		if c := comment(fd); c != "" {
			schema["description"] = c
		}
		props[string(fd.Name())] = schema
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func fieldSchema(fd protoreflect.FieldDescriptor) map[string]any {
	switch fd.Kind() {
	case protoreflect.MessageKind:
		return messageSchema(fd.Message())
	case protoreflect.BoolKind:
		return map[string]any{"type": "boolean"}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return map[string]any{"type": "number"}
	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.EnumKind:
		return map[string]any{"type": "string"}
	default:
		return map[string]any{"type": "integer"}
	}
}

// messageValue は m を JSON にする値に直す。protojson は int64 を文字列にするので使わない。金額は
// 数として渡したい。キーは proto の項目名、未設定の optional は出さない。
func messageValue(m protoreflect.Message) map[string]any {
	out := map[string]any{}
	fields := m.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		name := string(fd.Name())
		switch {
		case fd.IsList():
			list := m.Get(fd).List()
			items := make([]any, list.Len())
			for j := range list.Len() {
				items[j] = scalarValue(fd, list.Get(j))
			}
			out[name] = items
		case fd.HasOptionalKeyword() && !m.Has(fd):
		default:
			out[name] = scalarValue(fd, m.Get(fd))
		}
	}
	return out
}

func scalarValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	switch fd.Kind() {
	case protoreflect.MessageKind:
		return messageValue(v.Message())
	case protoreflect.EnumKind:
		return string(fd.Enum().Values().ByNumber(v.Enum()).Name())
	default:
		return v.Interface()
	}
}
