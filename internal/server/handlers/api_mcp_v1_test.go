package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

func mcpPost(t *testing.T, p *storage.APIKeyPrincipal, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v2/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer gotodo_test")
	if p != nil {
		r = utils.WithAPIKeyPrincipal(r, p)
	}
	w := httptest.NewRecorder()
	APIV1MCP(w, r)
	return w
}

func agentPrincipalForTest() *storage.APIKeyPrincipal {
	return &storage.APIKeyPrincipal{KeyID: 1, UserID: 50, ProjectID: 10, IsAgent: true,
		Scopes: []string{storage.APIScopeTasksRead}}
}

func decodeMCP(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func TestMCPRejectsNonAgentCallers(t *testing.T) {
	if w := mcpPost(t, nil, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); w.Code != http.StatusForbidden {
		t.Fatalf("no principal: %d", w.Code)
	}
	manager := &storage.APIKeyPrincipal{UserID: 7, ProjectID: 10, Scopes: []string{storage.APIScopeTasksRead}}
	if w := mcpPost(t, manager, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); w.Code != http.StatusForbidden {
		t.Fatalf("manager project key: %d", w.Code)
	}
}

func TestMCPInitializeAndListTools(t *testing.T) {
	p := agentPrincipalForTest()
	w := mcpPost(t, p, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("initialize: %d %s", w.Code, w.Body.String())
	}
	res := decodeMCP(t, w)["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Fatalf("protocolVersion=%v, want the client's supported version echoed", res["protocolVersion"])
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatal("tools capability missing")
	}

	// Unknown versions fall back to the newest we support.
	w = mcpPost(t, p, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := decodeMCP(t, w)["result"].(map[string]any)["protocolVersion"]; v != mcpProtocolVersions[0] {
		t.Fatalf("fallback version=%v", v)
	}

	if w := mcpPost(t, p, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Fatalf("notification: %d %q", w.Code, w.Body.String())
	}

	w = mcpPost(t, p, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	tools := decodeMCP(t, w)["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = true
		if _, ok := tool["inputSchema"].(map[string]any); !ok {
			t.Errorf("%v has no inputSchema", tool["name"])
		}
	}
	for _, want := range []string{"get_agent_context", "list_my_runs", "start_run", "finish_run", "get_task", "add_comment", "move_task", "update_task"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}

	w = mcpPost(t, p, `{"jsonrpc":"2.0","id":4,"method":"resources/list"}`)
	if e, ok := decodeMCP(t, w)["error"].(map[string]any); !ok || e["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %s", w.Body.String())
	}
}

func TestMCPToolCallsReplayRESTAsCaller(t *testing.T) {
	type seen struct{ method, path, auth, body string }
	var got []seen
	prev := MCPSubrequestHandler
	MCPSubrequestHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), string(b)})
		if strings.HasPrefix(r.URL.Path, "/api/v2/tasks/9") {
			utils.APIJSONError(w, http.StatusForbidden, "agent_guardrail", "This agent is not allowed to change title.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	t.Cleanup(func() { MCPSubrequestHandler = prev })
	p := agentPrincipalForTest()

	call := func(id int, name, args string) map[string]any {
		t.Helper()
		w := mcpPost(t, p, `{"jsonrpc":"2.0","id":`+itoa(id)+`,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
		return decodeMCP(t, w)["result"].(map[string]any)
	}

	if res := call(1, "move_task", `{"task_id":5,"status_id":2}`); res["isError"] != false {
		t.Fatalf("move_task: %+v", res)
	}
	if res := call(2, "list_my_runs", `{"status":"queued,running"}`); res["isError"] != false {
		t.Fatalf("list_my_runs: %+v", res)
	}
	res := call(3, "update_task", `{"task_id":9,"changes":{"title":"x"}}`)
	if res["isError"] != true {
		t.Fatalf("guardrail error not surfaced: %+v", res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != "HTTP 403 — agent_guardrail: This agent is not allowed to change title." {
		t.Fatalf("error text = %q", text)
	}
	// Bad arguments never reach the API.
	if res := call(4, "get_task", `{"task_id":"abc"}`); res["isError"] != true {
		t.Fatalf("bad args: %+v", res)
	}

	want := []seen{
		{http.MethodPatch, "/api/v2/tasks/5", "Bearer gotodo_test", `{"status_id":2}`},
		{http.MethodGet, "/api/v2/agent/runs?status=queued%2Crunning", "Bearer gotodo_test", ""},
		{http.MethodPatch, "/api/v2/tasks/9", "Bearer gotodo_test", `{"title":"x"}`},
	}
	if len(got) != len(want) {
		t.Fatalf("subrequests = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("subrequest %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
