package mcpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boykush/finlake/internal/ledger"
	"github.com/boykush/finlake/internal/ledger/ledgertest"
)

// newServer はサンプルの明細を引く MCP サーバーを返す。
func newServer(t *testing.T, cfg Config) *mcp.Server {
	t.Helper()
	s, err := NewServer(ledger.NewService(ledger.NewStore(ledgertest.Lake(t))), "test", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func post(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(Handler(newServer(t, Config{})))
	defer srv.Close()

	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_monthly_summary","arguments":{"month":"2026-09"}}}`

	resp, body := post(t, srv.URL+"/mcp", call)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if strings.Contains(body, `"isError":true`) || !strings.Contains(body, `"income":300000`) {
		t.Errorf("body = %s", body)
	}

	health, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d", health.StatusCode)
	}
}
