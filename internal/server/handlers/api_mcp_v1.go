package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"GoTodo/internal/version"
)

// MCP (Model Context Protocol) server for AI agent keys, over the streamable
// HTTP transport with plain JSON responses. Every tool is replayed as an
// in-process REST call with the caller's own bearer key, so MCP is held to the
// exact same allowlist, agent guardrails, and rate limits as the REST API.

// MCPSubrequestHandler serves the REST calls MCP tools make. The server sets
// it to the real mux; tests can swap it.
var MCPSubrequestHandler http.Handler = http.DefaultServeMux

var mcpProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

const mcpServerInstructions = `You are an AI agent working inside a GoTodo project. ` +
	`Call get_agent_context first to learn your instructions, guardrails, and the board's statuses. ` +
	`Then list_my_runs to find queued work. For each run: start_run, read the task with get_task, ` +
	`do the work, post what you did with add_comment, move or update the task within your guardrails, ` +
	`and finish_run with a short summary. Edits outside your guardrails are refused with an agent_guardrail error.`

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	call        func(args map[string]any) mcpCall
}

// mcpCall is the REST request a tool maps to.
type mcpCall struct {
	method string
	path   string // after /api/v2
	body   any
	err    string // argument error; no request is made
}

func objSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var (
	idProp     = map[string]any{"type": "integer", "minimum": 1}
	stringProp = func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
)

func argInt(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		if v >= 1 && v == float64(int(v)) {
			return int(v), true
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

func argString(args map[string]any, key string) string {
	if s, ok := args[key].(string); ok {
		return s
	}
	return ""
}

var mcpTools = []mcpTool{
	{
		Name:        "get_agent_context",
		Title:       "Who am I",
		Description: "Your agent profile: name, standing instructions from the project managers, guardrails (which fields and statuses you may change), and the board's statuses with their ids.",
		InputSchema: objSchema(map[string]any{}),
		call:        func(map[string]any) mcpCall { return mcpCall{method: http.MethodGet, path: "/agent"} },
	},
	{
		Name:        "list_my_runs",
		Title:       "List my runs",
		Description: "Runs assigned to you. Defaults to open runs (queued and running). Each run names a task_id, why it was triggered, and an optional note from the person who sent it.",
		InputSchema: objSchema(map[string]any{
			"status": stringProp("Comma-separated statuses: queued, running, succeeded, failed, cancelled."),
		}),
		call: func(args map[string]any) mcpCall {
			p := "/agent/runs"
			if s := strings.TrimSpace(argString(args, "status")); s != "" {
				p += "?status=" + url.QueryEscape(s)
			}
			return mcpCall{method: http.MethodGet, path: p}
		},
	},
	{
		Name:        "start_run",
		Title:       "Start a run",
		Description: "Mark a queued run as running before you begin work on it.",
		InputSchema: objSchema(map[string]any{"run_id": idProp}, "run_id"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "run_id")
			if !ok {
				return mcpCall{err: "run_id must be a positive integer"}
			}
			return mcpCall{method: http.MethodPost, path: "/agent/runs/" + strconv.Itoa(id) + "/start"}
		},
	},
	{
		Name:        "finish_run",
		Title:       "Finish a run",
		Description: "Close a run as succeeded or failed with a short summary of what you did (or why you could not). This also releases your claim on the task.",
		InputSchema: objSchema(map[string]any{
			"run_id":  idProp,
			"status":  map[string]any{"type": "string", "enum": []string{"succeeded", "failed"}},
			"summary": stringProp("What you did, in a sentence or two."),
		}, "run_id", "status"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "run_id")
			if !ok {
				return mcpCall{err: "run_id must be a positive integer"}
			}
			return mcpCall{method: http.MethodPost, path: "/agent/runs/" + strconv.Itoa(id) + "/finish", body: map[string]any{
				"status": argString(args, "status"), "summary": argString(args, "summary"),
			}}
		},
	},
	{
		Name:        "get_task",
		Title:       "Read a task",
		Description: "A task's full details (title, description, status, priority, due date, tags, custom fields).",
		InputSchema: objSchema(map[string]any{"task_id": idProp}, "task_id"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "task_id")
			if !ok {
				return mcpCall{err: "task_id must be a positive integer"}
			}
			return mcpCall{method: http.MethodGet, path: "/tasks/" + strconv.Itoa(id)}
		},
	},
	{
		Name:        "get_task_comments",
		Title:       "Read a task's discussion",
		Description: "The discussion thread on a task, oldest first. Acceptance criteria and follow-up requests are often here.",
		InputSchema: objSchema(map[string]any{"task_id": idProp}, "task_id"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "task_id")
			if !ok {
				return mcpCall{err: "task_id must be a positive integer"}
			}
			return mcpCall{method: http.MethodGet, path: "/tasks/" + strconv.Itoa(id) + "/comments"}
		},
	},
	{
		Name:        "list_tasks",
		Title:       "List tasks",
		Description: "Tasks in your project, 50 per page.",
		InputSchema: objSchema(map[string]any{
			"search":    stringProp("Text to match in titles and descriptions."),
			"completed": map[string]any{"type": "string", "enum": []string{"complete", "incomplete"}},
			"page":      idProp,
		}),
		call: func(args map[string]any) mcpCall {
			q := url.Values{}
			if s := strings.TrimSpace(argString(args, "search")); s != "" {
				q.Set("search", s)
			}
			if s := argString(args, "completed"); s != "" {
				q.Set("completed", s)
			}
			if n, ok := argInt(args, "page"); ok {
				q.Set("page", strconv.Itoa(n))
			}
			p := "/tasks"
			if len(q) > 0 {
				p += "?" + q.Encode()
			}
			return mcpCall{method: http.MethodGet, path: p}
		},
	},
	{
		Name:        "add_comment",
		Title:       "Comment on a task",
		Description: "Post a comment on a task (Markdown). Use it to report what you did, ask a question, or explain why you stopped.",
		InputSchema: objSchema(map[string]any{"task_id": idProp, "body": stringProp("Comment text (Markdown).")}, "task_id", "body"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "task_id")
			if !ok {
				return mcpCall{err: "task_id must be a positive integer"}
			}
			body := strings.TrimSpace(argString(args, "body"))
			if body == "" {
				return mcpCall{err: "body is required"}
			}
			return mcpCall{method: http.MethodPost, path: "/tasks/" + strconv.Itoa(id) + "/comments", body: map[string]any{"body": body}}
		},
	},
	{
		Name:        "move_task",
		Title:       "Move a task",
		Description: "Move a task to another board status by status_id (see get_agent_context). Only statuses your guardrails allow are accepted.",
		InputSchema: objSchema(map[string]any{"task_id": idProp, "status_id": idProp}, "task_id", "status_id"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "task_id")
			sid, ok2 := argInt(args, "status_id")
			if !ok || !ok2 {
				return mcpCall{err: "task_id and status_id must be positive integers"}
			}
			return mcpCall{method: http.MethodPatch, path: "/tasks/" + strconv.Itoa(id), body: map[string]any{"status_id": sid}}
		},
	},
	{
		Name:  "update_task",
		Title: "Update a task",
		Description: "Change task fields. `changes` takes the same keys as PATCH /api/v2/tasks/{id}: title, description, " +
			"priority (0-3), due_date (YYYY-MM-DD), tag_ids, estimate_points, sprint_id, fields (custom fields), status_id, completed. " +
			"Only fields your guardrails allow are accepted.",
		InputSchema: objSchema(map[string]any{
			"task_id": idProp,
			"changes": map[string]any{"type": "object", "description": "Fields to change."},
		}, "task_id", "changes"),
		call: func(args map[string]any) mcpCall {
			id, ok := argInt(args, "task_id")
			if !ok {
				return mcpCall{err: "task_id must be a positive integer"}
			}
			changes, ok := args["changes"].(map[string]any)
			if !ok || len(changes) == 0 {
				return mcpCall{err: "changes must be a non-empty object"}
			}
			return mcpCall{method: http.MethodPatch, path: "/tasks/" + strconv.Itoa(id), body: changes}
		},
	},
	{
		Name:        "create_task",
		Title:       "Create a task",
		Description: "Create a task or subtask in your project. Only available if your guardrails allow creating tasks.",
		InputSchema: objSchema(map[string]any{
			"project_id":  idProp,
			"title":       stringProp("Task title."),
			"description": stringProp("Task description (Markdown)."),
			"parent_id":   idProp,
		}, "project_id", "title"),
		call: func(args map[string]any) mcpCall {
			pid, ok := argInt(args, "project_id")
			title := strings.TrimSpace(argString(args, "title"))
			if !ok || title == "" {
				return mcpCall{err: "project_id and title are required"}
			}
			body := map[string]any{"project_id": pid, "title": title}
			if d := argString(args, "description"); d != "" {
				body["description"] = d
			}
			if parent, ok := argInt(args, "parent_id"); ok {
				body["parent_id"] = parent
			}
			return mcpCall{method: http.MethodPost, path: "/tasks", body: body}
		},
	},
}

func findMCPTool(name string) *mcpTool {
	for i := range mcpTools {
		if mcpTools[i].Name == name {
			return &mcpTools[i]
		}
	}
	return nil
}

// APIV1MCP handles POST /api/v2/mcp for agent keys.
func APIV1MCP(w http.ResponseWriter, r *http.Request) {
	if _, ok := agentPrincipal(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		methodNotAllowed(w)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &mcpError{Code: -32700, Message: "Parse error"}})
		return
	}
	raw = bytes.TrimSpace(raw)

	// Batches were dropped from the 2025-06-18 spec, but older clients may send them.
	if len(raw) > 0 && raw[0] == '[' {
		var reqs []mcpRequest
		if err := json.Unmarshal(raw, &reqs); err != nil {
			writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &mcpError{Code: -32700, Message: "Parse error"}})
			return
		}
		out := make([]mcpResponse, 0, len(reqs))
		for _, req := range reqs {
			if resp, ok := handleMCPRequest(r, req); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	var req mcpRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &mcpError{Code: -32700, Message: "Parse error"}})
		return
	}
	resp, ok := handleMCPRequest(r, req)
	if !ok {
		w.WriteHeader(http.StatusAccepted) // notification: no response body
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleMCPRequest returns false for notifications, which get no response.
func handleMCPRequest(r *http.Request, req mcpRequest) (mcpResponse, bool) {
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return mcpResponse{}, false
	}
	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		pv := mcpProtocolVersions[0]
		for _, v := range mcpProtocolVersions {
			if v == p.ProtocolVersion {
				pv = v
				break
			}
		}
		resp.Result = map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "gotodo", "title": "GoTodo", "version": version.Version},
			"instructions":    mcpServerInstructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		tools := make([]mcpTool, len(mcpTools))
		copy(tools, mcpTools)
		resp.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &mcpError{Code: -32602, Message: "Invalid params"}
			break
		}
		tool := findMCPTool(p.Name)
		if tool == nil {
			resp.Error = &mcpError{Code: -32602, Message: "Unknown tool: " + p.Name}
			break
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		resp.Result = runMCPTool(r, tool, p.Arguments)
	default:
		resp.Error = &mcpError{Code: -32601, Message: "Method not found: " + req.Method}
	}
	return resp, true
}

func mcpToolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func runMCPTool(parent *http.Request, tool *mcpTool, args map[string]any) map[string]any {
	call := tool.call(args)
	if call.err != "" {
		return mcpToolResult(call.err, true)
	}
	status, body := mcpSubrequest(parent, call)
	if status >= 200 && status < 300 {
		if len(bytes.TrimSpace(body)) == 0 {
			return mcpToolResult("OK", false)
		}
		return mcpToolResult(string(body), false)
	}
	// Same envelope as utils.APIJSONError.
	var apiErr struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		msg = apiErr.Error + ": " + apiErr.Message
	}
	return mcpToolResult(fmt.Sprintf("HTTP %d — %s", status, msg), true)
}

// mcpSubrequest replays call through the REST API as the same caller.
func mcpSubrequest(parent *http.Request, call mcpCall) (int, []byte) {
	var rdr io.Reader
	if call.body != nil {
		raw, err := json.Marshal(call.body)
		if err != nil {
			return http.StatusBadRequest, []byte(`{"error":{"code":"invalid_request","message":"bad arguments"}}`)
		}
		rdr = bytes.NewReader(raw)
	}
	// Routes are registered with and without BASE_PATH, so the bare path works.
	target := "/api/v2" + call.path
	req, err := http.NewRequestWithContext(parent.Context(), call.method, target, rdr)
	if err != nil {
		return http.StatusBadRequest, []byte(`{"error":{"code":"invalid_request","message":"bad request"}}`)
	}
	req.Header.Set("Authorization", parent.Header.Get("Authorization"))
	if call.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Body == nil {
		req.Body = http.NoBody // handlers, like net/http's server, expect a non-nil body
	}
	req.RemoteAddr = parent.RemoteAddr
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		if v := parent.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	rec := &mcpRecorder{header: http.Header{}, status: http.StatusOK}
	MCPSubrequestHandler.ServeHTTP(rec, req)
	return rec.status, rec.body.Bytes()
}

type mcpRecorder struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (m *mcpRecorder) Header() http.Header { return m.header }

func (m *mcpRecorder) WriteHeader(code int) {
	if !m.wroteHeader {
		m.status = code
		m.wroteHeader = true
	}
}

func (m *mcpRecorder) Write(b []byte) (int, error) {
	m.wroteHeader = true
	return m.body.Write(b)
}
