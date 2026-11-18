// MCP over streamable HTTP: JSON-RPC 2.0 with plain JSON responses,
// six tools acting on the same store as REST.

package main

import (
	"math"
	"strings"
)

func mcpServerInfoCopy() *JObj {
	return NewObj(KV("name", "forge-mcp"), KV("version", "1.0.0"))
}

// Precomputed once: tools/list must be byte-identical across calls.
var mcpTools = []JVal{
	NewObj(
		KV("name", "create_repo"),
		KV("description", "Create a new repository owned by the authenticated user."),
		KV("inputSchema", NewObj(
			KV("type", "object"),
			KV("properties", NewObj(
				KV("name", NewObj(KV("type", "string"), KV("description", "Repository name."))),
				KV("description", NewObj(KV("type", "string"), KV("description", "Repository description."))),
				KV("private", NewObj(KV("type", "boolean"), KV("description", "Whether the repository is private."))),
			)),
			KV("required", []JVal{"name"}),
		)),
	),
	NewObj(
		KV("name", "get_repo"),
		KV("description", "Get a repository by owner and name."),
		KV("inputSchema", NewObj(
			KV("type", "object"),
			KV("properties", NewObj(
				KV("owner", NewObj(KV("type", "string"), KV("description", "Repository owner login."))),
				KV("repo", NewObj(KV("type", "string"), KV("description", "Repository name."))),
			)),
			KV("required", []JVal{"owner", "repo"}),
		)),
	),
	NewObj(
		KV("name", "create_issue"),
		KV("description", "Create an issue in a repository."),
		KV("inputSchema", NewObj(
			KV("type", "object"),
			KV("properties", NewObj(
				KV("owner", NewObj(KV("type", "string"), KV("description", "Repository owner login."))),
				KV("repo", NewObj(KV("type", "string"), KV("description", "Repository name."))),
				KV("title", NewObj(KV("type", "string"), KV("description", "Issue title."))),
				KV("body", NewObj(KV("type", "string"), KV("description", "Issue body."))),
				KV("labels", NewObj(KV("type", "array"), KV("items", NewObj(KV("type", "string"))), KV("description", "Label names."))),
			)),
			KV("required", []JVal{"owner", "repo", "title"}),
		)),
	),
	NewObj(
		KV("name", "list_issues"),
		KV("description", "List issues in a repository."),
		KV("inputSchema", NewObj(
			KV("type", "object"),
			KV("properties", NewObj(
				KV("owner", NewObj(KV("type", "string"), KV("description", "Repository owner login."))),
				KV("repo", NewObj(KV("type", "string"), KV("description", "Repository name."))),
				KV("state", NewObj(KV("type", "string"), KV("enum", []JVal{"open", "closed", "all"}), KV("description", "Issue state filter."))),
			)),
			KV("required", []JVal{"owner", "repo"}),
		)),
	),
	NewObj(
		KV("name", "create_pull_request"),
		KV("description", "Create a pull request between two branches."),
		KV("inputSchema", NewObj(
			KV("type", "object"),
			KV("properties", NewObj(
				KV("owner", NewObj(KV("type", "string"), KV("description", "Repository owner login."))),
				KV("repo", NewObj(KV("type", "string"), KV("description", "Repository name."))),
				KV("title", NewObj(KV("type", "string"), KV("description", "Pull request title."))),
				KV("body", NewObj(KV("type", "string"), KV("description", "Pull request body."))),
				KV("head", NewObj(KV("type", "string"), KV("description", "Head branch name."))),
				KV("base", NewObj(KV("type", "string"), KV("description", "Base branch name."))),
			)),
			KV("required", []JVal{"owner", "repo", "title", "head", "base"}),
		)),
	),
	NewObj(
		KV("name", "merge_pull_request"),
		KV("description", "Merge a pull request with a squash merge."),
		KV("inputSchema", NewObj(
			KV("type", "object"),
			KV("properties", NewObj(
				KV("owner", NewObj(KV("type", "string"), KV("description", "Repository owner login."))),
				KV("repo", NewObj(KV("type", "string"), KV("description", "Repository name."))),
				KV("pull_number", NewObj(KV("type", "integer"), KV("description", "Pull request number."))),
			)),
			KV("required", []JVal{"owner", "repo", "pull_number"}),
		)),
	),
}

type mcpArgError struct{ msg string }

func (e *mcpArgError) Error() string { return e.msg }

func rpcResult(id JVal, result JVal) *JObj {
	return NewObj(KV("jsonrpc", "2.0"), KV("id", id), KV("result", result))
}

func rpcError(id JVal, code int, message string) *JObj {
	return NewObj(KV("jsonrpc", "2.0"), KV("id", id), KV("error", NewObj(KV("code", int64(code)), KV("message", message))))
}

func normalizeToolName(raw string) (string, bool) {
	last := raw
	if idx := strings.LastIndex(raw, "__"); idx >= 0 {
		last = raw[idx+2:]
	}
	name := strings.ToLower(strings.ReplaceAll(last, "-", "_"))
	aliases := map[string]string{
		"create_repo":            "create_repo",
		"create_repository":      "create_repo",
		"get_repo":               "get_repo",
		"get_repository":         "get_repo",
		"create_issue":           "create_issue",
		"list_issues":            "list_issues",
		"list_repository_issues": "list_issues",
		"create_pull_request":    "create_pull_request",
		"create_pullrequest":     "create_pull_request",
		"merge_pull_request":     "merge_pull_request",
		"merge_pullrequest":      "merge_pull_request",
	}
	canonical, ok := aliases[name]
	return canonical, ok
}

func requireMcpArg(args map[string]interface{}, keys []string, label string) (string, error) {
	for _, k := range keys {
		v := args[k]
		if s, ok := v.(string); ok && len(s) > 0 {
			return s, nil
		}
		if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return jsStringOfNumber(f), nil
		}
		if n, ok := v.(int64); ok {
			return jsStringOfNumber(float64(n)), nil
		}
	}
	return "", &mcpArgError{label + " is required"}
}

func resolveMcpRepo(args map[string]interface{}) (owner, name string, err error) {
	owner = asString(args["owner"], "")
	if owner == "" {
		owner = asString(args["owner_login"], "")
	}
	name = asString(args["repo"], "")
	if name == "" {
		name = asString(args["repository"], "")
	}
	if owner == "" || name == "" {
		if full, ok := args["full_name"].(string); ok {
			parts := strings.Split(full, "/")
			if len(parts) == 2 {
				if owner == "" {
					owner = parts[0]
				}
				if name == "" {
					name = parts[1]
				}
			}
		}
	}
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		if owner == "" {
			owner = parts[0]
		}
		name = strings.Join(parts[1:], "/")
	}
	if owner == "" || name == "" {
		return "", "", &mcpArgError{"owner and repo are required"}
	}
	return owner, name, nil
}

func toolOk(payload JVal) *JObj {
	return NewObj(
		KV("content", []JVal{NewObj(KV("type", "text"), KV("text", string(MarshalJSON(payload))))}),
		KV("structuredContent", payload),
	)
}

func toolErr(message string) *JObj {
	return NewObj(
		KV("content", []JVal{NewObj(KV("type", "text"), KV("text", message))}),
		KV("structuredContent", NewObj(KV("error", message))),
		KV("isError", true),
	)
}

func dispatchTool(tool string, args map[string]interface{}) (*JObj, error) {
	switch tool {
	case "create_repo":
		name, err := requireMcpArg(args, []string{"name", "repository", "repo"}, "name")
		if err != nil {
			return nil, err
		}
		if _, exists := reposByKey[repoKey(viewerLogin, name)]; exists {
			return toolErr("Repository '" + name + "' already exists"), nil
		}
		defBranch := "main"
		if s, ok := args["default_branch"].(string); ok && len(s) > 0 {
			defBranch = s
		}
		r := newRepo(name, viewerLogin, asString(args["description"], ""), args["private"] == true, defBranch)
		reposByKey[repoKey(r.owner, r.name)] = r
		reposByID[r.dbID] = r
		return toolOk(repoJSON(r)), nil
	case "get_repo":
		owner, name, err := resolveMcpRepo(args)
		if err != nil {
			return nil, err
		}
		r, ok := reposByKey[repoKey(owner, name)]
		if !ok {
			return toolErr("Repository not found"), nil
		}
		return toolOk(repoJSON(r)), nil
	case "create_issue":
		owner, name, err := resolveMcpRepo(args)
		if err != nil {
			return nil, err
		}
		title, err := requireMcpArg(args, []string{"title"}, "title")
		if err != nil {
			return nil, err
		}
		r, ok := reposByKey[repoKey(owner, name)]
		if !ok {
			return toolErr("Repository not found"), nil
		}
		labelNames := parseLabelInput(args["labels"])
		for _, n := range labelNames {
			if _, ok := r.labels[n]; !ok {
				return toolErr("Label '" + n + "' does not exist"), nil
			}
		}
		issue := createIssueRecord(r, title, asString(args["body"], ""))
		issue.labels = labelNames
		return toolOk(issueObject(r, issue)), nil
	case "list_issues":
		owner, name, err := resolveMcpRepo(args)
		if err != nil {
			return nil, err
		}
		r, ok := reposByKey[repoKey(owner, name)]
		if !ok {
			return toolErr("Repository not found"), nil
		}
		state := asString(args["state"], "open")
		var list []*issueRec
		for _, issue := range r.issues {
			if state != "all" && issue.state != state {
				continue
			}
			list = append(list, issue)
		}
		sortIssuesDesc(list)
		var items []JVal
		for _, issue := range list {
			items = append(items, issueObject(r, issue))
		}
		if items == nil {
			items = []JVal{}
		}
		return toolOk(items), nil
	case "create_pull_request":
		owner, name, err := resolveMcpRepo(args)
		if err != nil {
			return nil, err
		}
		title, err := requireMcpArg(args, []string{"title"}, "title")
		if err != nil {
			return nil, err
		}
		headRaw, err := requireMcpArg(args, []string{"head", "head_ref", "headRef", "headRefName"}, "head")
		if err != nil {
			return nil, err
		}
		baseRaw, err := requireMcpArg(args, []string{"base", "base_ref", "baseRef", "baseRefName"}, "base")
		if err != nil {
			return nil, err
		}
		headRef := stripBranchPrefix(headRaw)
		baseRef := stripBranchPrefix(baseRaw)
		r, ok := reposByKey[repoKey(owner, name)]
		if !ok {
			return toolErr("Repository not found"), nil
		}
		if _, ok := r.refs["refs/heads/"+headRef]; !ok {
			return toolErr("head branch '" + headRef + "' does not exist"), nil
		}
		if _, ok := r.refs["refs/heads/"+baseRef]; !ok {
			return toolErr("base branch '" + baseRef + "' does not exist"), nil
		}
		pull := createPullRecord(r, title, asString(args["body"], ""), headRef, baseRef)
		return toolOk(pullObject(r, pull)), nil
	case "merge_pull_request":
		owner, name, err := resolveMcpRepo(args)
		if err != nil {
			return nil, err
		}
		numRaw, err := requireMcpArg(args, []string{"pull_number", "pullNumber", "number"}, "pull_number")
		if err != nil {
			return nil, err
		}
		num, ok := jsParseInt(numRaw)
		if !ok || num < 1 {
			return nil, &mcpArgError{"pull_number must be a positive integer"}
		}
		r, ok := reposByKey[repoKey(owner, name)]
		if !ok {
			return toolErr("Repository not found"), nil
		}
		pull, ok := r.pulls[int(num)]
		if !ok {
			return toolErr("Pull request not found"), nil
		}
		sha, err := squashMergePull(r, pull)
		if err != nil {
			return toolErr(err.Error()), nil
		}
		return toolOk(NewObj(
			KV("merged", true),
			KV("sha", sha),
			KV("message", "Pull Request successfully merged"),
			KV("number", int64(pull.number)),
			KV("state", "closed"),
		)), nil
	}
	return toolErr("Unknown tool: " + tool), nil
}

func sortIssuesDesc(list []*issueRec) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].createdSeq > list[j-1].createdSeq; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// handleMcpMessage returns nil for notifications (no response ever sent).
func handleMcpMessage(msg interface{}) *JObj {
	rec, ok := msg.(map[string]interface{})
	if !ok {
		return rpcError(nil, -32600, "Invalid Request")
	}
	var id JVal
	hasID := false
	if raw, present := rec["id"]; present && raw != nil {
		hasID = true
		id = toJVal(raw)
	}
	method, isStr := rec["method"].(string)
	if !isStr {
		if !hasID {
			return nil
		}
		return rpcError(id, -32600, "Invalid Request")
	}
	if !hasID {
		return nil
	}
	params := asMap(rec["params"])
	if params == nil {
		params = map[string]interface{}{}
	}
	switch method {
	case "server/discover":
		return rpcResult(id, NewObj(
			KV("supportedVersions", []JVal{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}),
			KV("capabilities", NewObj(KV("tools", NewObj()))),
			KV("serverInfo", mcpServerInfoCopy()),
			KV("_meta", NewObj(KV("io.modelcontextprotocol/serverInfo", mcpServerInfoCopy()))),
		))
	case "initialize":
		version := "2025-11-25"
		if s, ok := params["protocolVersion"].(string); ok {
			version = s
		}
		return rpcResult(id, NewObj(
			KV("protocolVersion", version),
			KV("capabilities", NewObj(KV("tools", NewObj()))),
			KV("serverInfo", mcpServerInfoCopy()),
		))
	case "ping":
		return rpcResult(id, NewObj())
	case "tools/list":
		return rpcResult(id, NewObj(KV("tools", mcpTools)))
	case "tools/call":
		nameRaw, ok := params["name"].(string)
		if !ok || len(nameRaw) == 0 {
			return rpcError(id, -32602, "Tool name is required")
		}
		tool, ok := normalizeToolName(nameRaw)
		if !ok {
			return rpcError(id, -32601, "Unknown tool: "+nameRaw)
		}
		toolArgs := asMap(params["arguments"])
		if toolArgs == nil {
			toolArgs = map[string]interface{}{}
		}
		result, err := dispatchTool(tool, toolArgs)
		if err != nil {
			if ae, ok := err.(*mcpArgError); ok {
				return rpcError(id, -32602, ae.msg)
			}
			return rpcResult(id, toolErr(err.Error()))
		}
		return rpcResult(id, result)
	default:
		if strings.HasPrefix(method, "notifications/") {
			return rpcResult(id, NewObj())
		}
		return rpcError(id, -32601, "Method not found: "+method)
	}
}

// toJVal converts a parsed JSON value into a JVal for echoing (RPC ids).
func toJVal(v interface{}) JVal {
	switch t := v.(type) {
	case nil, bool, string:
		return t
	case float64:
		return t
	case int64:
		return t
	default:
		return nil
	}
}

func handleMcp(req *request) response {
	method := req.method
	if method == "GET" || (method != "POST" && method != "DELETE") {
		return sendError(405, "Method Not Allowed")
	}
	if method == "DELETE" {
		return sendNoContent()
	}
	if !isAuthorized(req) {
		return sendJSON(401, NewObj(
			KV("jsonrpc", "2.0"),
			KV("id", nil),
			KV("error", NewObj(KV("code", int64(-32000)), KV("message", "Unauthorized"))),
		))
	}
	parsed, err := parseJSONStrict(req.body)
	if err != nil {
		return sendJSON(400, NewObj(
			KV("jsonrpc", "2.0"),
			KV("id", nil),
			KV("error", NewObj(KV("code", int64(-32700)), KV("message", "Parse error"))),
		))
	}
	if arr, ok := parsed.([]interface{}); ok {
		var responses []JVal
		for _, item := range arr {
			if r := handleMcpMessage(item); r != nil {
				responses = append(responses, r)
			}
		}
		if len(responses) == 0 {
			return response{status: 202, headers: []headerPair{{"x-github-request-id", nextRequestID()}, {"content-length", "0"}}}
		}
		return sendJSON(200, responses)
	}
	resp := handleMcpMessage(parsed)
	if resp == nil {
		return response{status: 202, headers: []headerPair{{"x-github-request-id", nextRequestID()}, {"content-length", "0"}}}
	}
	return sendJSON(200, resp)
}
