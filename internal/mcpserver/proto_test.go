package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolsComeFromProto は、tool の名前・説明・入出力の形が proto から来ていることを確かめる。
func TestToolsComeFromProto(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(Handler(newServer(t, Config{})))
	defer srv.Close()
	cs := connectHTTP(t, srv.URL+"/mcp")

	if got := cs.InitializeResult().Instructions; !strings.Contains(got, "振替") {
		t.Errorf("instructions = %q", got)
	}

	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var listTransactions *mcp.Tool
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s = %+v", tool.Name, tool)
		}
		if tool.Name == "list_transactions" {
			listTransactions = tool
		}
	}
	slices.Sort(names)
	if want := []string{"get_category_trend", "get_monthly_summary", "list_daily_expenses", "list_months", "list_transactions"}; !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	schema, _ := json.Marshal(listTransactions.InputSchema)
	for _, want := range []string{`"description":"対象月（YYYY-MM）。from / to と合わせては使えない"`, `"limit":{"description":"最大件数（既定 200、上限 1000）","type":"integer"}`} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("input schema = %s, want %s", schema, want)
		}
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_transactions", Arguments: map[string]any{"month": "2026-09", "keyword": "買い出し"}})
	if err != nil || res.IsError {
		t.Fatalf("list_transactions: res = %+v, err = %v", res, err)
	}
	// 金額は数のまま、未設定の optional（ここでは無い）は出さず、キーは proto の項目名。
	out, _ := json.Marshal(res.StructuredContent)
	if want := `{"transactions":[{"account":"カード","amount":-3200,"category":"食費","date":"2026-09-03","description":"スーパー","id":"a2","kind":"expense","memo":"週末の買い出し","subcategory":"食料品"}]}`; string(out) != want {
		t.Errorf("structured content = %s, want %s", out, want)
	}

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_transactions", Arguments: map[string]any{"month": "2026-09", "unknown": 1}})
	if err != nil || !res.IsError {
		t.Errorf("unknown argument: res = %+v, err = %v", res, err)
	}
}
