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

func localPrincipalForTest(limit int, scopes ...string) *storage.APIKeyPrincipal {
	return &storage.APIKeyPrincipal{KeyID: 2, UserID: 7, IsLocalAgent: true, LimitProjectID: limit, Scopes: scopes}
}

func mcpToolNames(t *testing.T, p *storage.APIKeyPrincipal) map[string]bool {
	t.Helper()
	w := mcpPost(t, p, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	names := map[string]bool{}
	for _, raw := range decodeMCP(t, w)["result"].(map[string]any)["tools"].([]any) {
		names[raw.(map[string]any)["name"].(string)] = true
	}
	return names
}

func TestMCPRejectsStandardPersonalKeys(t *testing.T) {
	personal := &storage.APIKeyPrincipal{UserID: 7}
	if w := mcpPost(t, personal, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); w.Code != http.StatusForbidden {
		t.Fatalf("standard personal key: %d", w.Code)
	}
}

func TestMCPLocalKeyToolsFollowScopes(t *testing.T) {
	readOnly := mcpToolNames(t, localPrincipalForTest(0, storage.APIScopeTasksRead))
	for _, want := range []string{"get_context", "get_project", "get_task", "get_task_comments", "list_tasks"} {
		if !readOnly[want] {
			t.Errorf("read-only key missing %s", want)
		}
	}
	for _, hidden := range []string{"add_comment", "update_task", "move_task", "create_task", "claim_task",
		"get_agent_context", "list_my_runs", "start_run", "finish_run"} {
		if readOnly[hidden] {
			t.Errorf("read-only key should not see %s", hidden)
		}
	}

	full := mcpToolNames(t, localPrincipalForTest(0, storage.APIScopeTasksRead, storage.APIScopeTasksWrite, storage.APIScopeCommentsWrite))
	for _, want := range []string{"add_comment", "update_task", "move_task", "create_task", "claim_task"} {
		if !full[want] {
			t.Errorf("full key missing %s", want)
		}
	}
	if full["list_my_runs"] || full["get_agent_context"] {
		t.Error("local keys never see the agent run queue")
	}

	// AI agents keep their own tool set.
	agent := mcpToolNames(t, agentPrincipalForTest())
	if agent["get_context"] || agent["get_project"] || agent["claim_task"] {
		t.Errorf("agent sees local-only tools: %v", agent)
	}

	// Calling a hidden tool is an unknown tool, and never reaches the API.
	w := mcpPost(t, localPrincipalForTest(0, storage.APIScopeTasksRead),
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add_comment","arguments":{"task_id":1,"body":"x"}}}`)
	if e, ok := decodeMCP(t, w)["error"].(map[string]any); !ok || e["code"].(float64) != -32602 {
		t.Fatalf("hidden tool call: %s", w.Body.String())
	}
}

func TestMCPLocalKeyInstructions(t *testing.T) {
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	text := func(p *storage.APIKeyPrincipal) string {
		return decodeMCP(t, mcpPost(t, p, init))["result"].(map[string]any)["instructions"].(string)
	}
	ro := text(localPrincipalForTest(10, storage.APIScopeTasksRead))
	if !strings.Contains(ro, "read-only") || !strings.Contains(ro, "limited to project 10") || strings.Contains(ro, "list_my_runs") {
		t.Errorf("read-only instructions: %s", ro)
	}
	rw := text(localPrincipalForTest(0, storage.APIScopeTasksRead, storage.APIScopeCommentsWrite))
	if !strings.Contains(rw, "unless the user asks") || strings.Contains(rw, "limited to project") {
		t.Errorf("write instructions: %s", rw)
	}
	if a := text(agentPrincipalForTest()); !strings.Contains(a, "list_my_runs") {
		t.Errorf("agent instructions changed: %s", a)
	}
}

func TestMCPLocalKeyCombinedCalls(t *testing.T) {
	var paths []string
	prev := MCPSubrequestHandler
	MCPSubrequestHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		switch {
		case strings.HasSuffix(r.URL.Path, "/comments"):
			writeJSON(w, http.StatusOK, []map[string]any{{"body": "criteria"}})
		case strings.HasPrefix(r.URL.Path, "/api/v2/tasks/404"):
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
		default:
			writeJSON(w, http.StatusOK, map[string]any{"id": 1})
		}
	})
	t.Cleanup(func() { MCPSubrequestHandler = prev })
	p := localPrincipalForTest(10, storage.APIScopeTasksRead)

	call := func(name, args string) map[string]any {
		t.Helper()
		w := mcpPost(t, p, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
		return decodeMCP(t, w)["result"].(map[string]any)
	}
	resultText := func(res map[string]any) string {
		return res["content"].([]any)[0].(map[string]any)["text"].(string)
	}

	res := call("get_task", `{"task_id":"#12","include_comments":true}`)
	if res["isError"] != false {
		t.Fatalf("get_task: %+v", res)
	}
	var combined map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &combined); err != nil {
		t.Fatalf("combined result: %v", err)
	}
	if _, ok := combined["task"]; !ok {
		t.Errorf("task missing: %v", combined)
	}
	if c, ok := combined["comments"].([]any); !ok || len(c) != 1 {
		t.Errorf("comments missing: %v", combined)
	}

	if res := call("get_task", `{"task_id":"http://localhost:8080/tasks/13"}`); resultText(res) != `{"id":1}` {
		t.Errorf("plain get_task should return the task as is: %+v", res)
	}
	if res := call("get_task", `{"task_id":404,"include_comments":true}`); res["isError"] != true ||
		resultText(res) != "HTTP 404 — not_found: Task not found." {
		t.Errorf("first failure should be the result: %+v", res)
	}
	if res := call("get_context", `{}`); res["isError"] != false {
		t.Errorf("get_context: %+v", res)
	}
	if res := call("get_project", `{"project_id":10}`); res["isError"] != false {
		t.Errorf("get_project: %+v", res)
	}
	if res := call("list_tasks", `{"mine":true,"project_id":10}`); res["isError"] != false {
		t.Errorf("list_tasks: %+v", res)
	}

	want := []string{
		"GET /api/v2/tasks/12", "GET /api/v2/tasks/12/comments",
		"GET /api/v2/tasks/13",
		"GET /api/v2/tasks/404",
		"GET /api/v2/me", "GET /api/v2/projects/10",
		"GET /api/v2/projects/10", "GET /api/v2/projects/10/statuses", "GET /api/v2/tags?project_id=10",
		"GET /api/v2/tasks?claimed=me&project=10",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Errorf("subrequests:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

func TestArgTaskIDAcceptsWhatUsersPaste(t *testing.T) {
	for in, want := range map[any]int{
		float64(5): 5, "5": 5, "#123": 123, " # 7 ": 7, "task 42": 42, "Task #43": 43,
		"https://todo.example.com/tasks/88": 88, "http://localhost:8080/app/tasks/9?tab=comments": 9, "/tasks/10": 10,
	} {
		if got, ok := argTaskID(map[string]any{"task_id": in}, "task_id"); !ok || got != want {
			t.Errorf("argTaskID(%v) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []any{"abc", "#", "-3", float64(0), float64(1.5), "https://x.com/projects/3", "https://x.com/tasks/abc", nil} {
		if got, ok := argTaskID(map[string]any{"task_id": in}, "task_id"); ok {
			t.Errorf("argTaskID(%v) = %d, want rejected", in, got)
		}
	}
}
