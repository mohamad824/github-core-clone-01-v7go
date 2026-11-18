// GraphQL executor: typed values, field resolution, connections, and
// the request entry point. Error shapes and field semantics mirror the
// reference implementation exactly.

package main

import (
	"encoding/base64"
	"sort"
	"strconv"
)

// gqlTV is a value tagged with its GraphQL type name. val is nil, bool,
// string, int64, []gqlTV (a list), or map[string]interface{} (an object
// payload whose fields resolvers read).
type gqlTV struct {
	typ string
	val interface{}
}

func tv(typ string, val interface{}) *gqlTV { return &gqlTV{typ: typ, val: val} }

type gqlCtx struct {
	variables map[string]interface{}
	fragments map[string]*gqlDefinition
	errors    []*JObj
}

var nodeTypes = []string{"Repository", "Issue", "PullRequest", "User", "IssueComment"}

func nodeTypeSet() map[string]bool {
	return map[string]bool{"Repository": true, "Issue": true, "PullRequest": true, "User": true, "IssueComment": true}
}

var nodeTypesSet = nodeTypeSet()

func valueFromAst(ast *gqlValue, variables map[string]interface{}) interface{} {
	if ast == nil {
		// Mirrors the reference: a missing directive argument value throws
		// a non-GraphQL error, which surfaces as a 500 via the dispatcher.
		panic("missing value in directive argument")
	}
	switch ast.kind {
	case "variable":
		return variables[ast.name]
	case "int":
		return ast.intVal
	case "float":
		return ast.floatVal
	case "string":
		return ast.strVal
	case "boolean":
		return ast.boolVal
	case "null":
		return nil
	case "enum":
		return ast.strVal
	case "list":
		out := make([]interface{}, len(ast.items))
		for i, it := range ast.items {
			out[i] = valueFromAst(it, variables)
		}
		return out
	case "object":
		out := map[string]interface{}{}
		for _, f := range ast.fields {
			out[f.name] = valueFromAst(f.value, variables)
		}
		return out
	}
	return nil
}

func resolveArgs(args map[string]*gqlValue, ctx *gqlCtx) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range args {
		out[k] = valueFromAst(v, ctx.variables)
	}
	return out
}

func skipByDirective(dirs []gqlDirective, ctx *gqlCtx) bool {
	for _, d := range dirs {
		if d.name == "skip" {
			if valueFromAst(d.args["if"], ctx.variables) == true {
				return true
			}
		} else if d.name == "include" {
			if valueFromAst(d.args["if"], ctx.variables) != true {
				return true
			}
		}
	}
	return false
}

func typeMatches(condition, actual string) bool {
	if condition == actual {
		return true
	}
	if condition == "Node" && nodeTypesSet[actual] {
		return true
	}
	return false
}

func mergeInto(target, source *JObj) {
	for _, p := range source.pairs {
		existing, ok := target.Get(p.k)
		if ok {
			eObj, eOK := existing.(*JObj)
			sObj, sOK := p.v.(*JObj)
			if eOK && sOK {
				merged := &JObj{pairs: append([]jpair{}, eObj.pairs...)}
				mergeInto(merged, sObj)
				target.Set(p.k, merged)
				continue
			}
		}
		target.Set(p.k, p.v)
	}
}

type gqlResolver func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error)

func cursorOfNumber(n int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(n)))
}

func numberFromCursor(cursor string) (int, bool) {
	decoded := string(lenientB64(cursor))
	n, ok := jsParseInt(decoded)
	if !ok {
		return 0, false
	}
	return int(n), true
}

func issueStateOf(issue *issueRec) string {
	if issue.state == "open" {
		return "OPEN"
	}
	return "CLOSED"
}

func pullStateOf(pull *pullRec) string {
	if pull.merged {
		return "MERGED"
	}
	if pull.state == "open" {
		return "OPEN"
	}
	return "CLOSED"
}

func userTypedValue() *gqlTV {
	return tv("User", map[string]interface{}{
		"login": scen.viewer.login, "id": scen.viewer.id, "name": scen.viewer.name, "email": scen.viewer.email,
	})
}

func issueTypedValue(r *repo, issue *issueRec) *gqlTV {
	return tv("Issue", map[string]interface{}{"repo": r, "issue": issue})
}

func pullTypedValue(r *repo, pull *pullRec) *gqlTV {
	return tv("PullRequest", map[string]interface{}{"repo": r, "pull": pull})
}

func repoTypedValue(r *repo) *gqlTV {
	return tv("Repository", map[string]interface{}{"repo": r})
}

func pageInfoValue(hasNext, hasPrev bool, start, end interface{}) map[string]interface{} {
	return map[string]interface{}{
		"hasNextPage": hasNext, "hasPreviousPage": hasPrev, "startCursor": start, "endCursor": end,
	}
}

func issuesConnection(r *repo, args map[string]interface{}) *gqlTV {
	var states []string
	if arr, ok := args["states"].([]interface{}); ok {
		for _, s := range arr {
			states = append(states, gqlStringOf(s))
		}
	} else {
		states = []string{"OPEN"}
	}
	direction := "ASC"
	if orderBy, ok := args["orderBy"].(map[string]interface{}); ok {
		if d, ok := orderBy["direction"]; ok {
			direction = gqlStringOf(d)
		}
	}
	var afterNum *int
	if afterRaw, ok := args["after"].(string); ok && afterRaw != "" {
		if n, ok := numberFromCursor(afterRaw); ok {
			afterNum = &n
		}
	}
	var list []*issueRec
	for _, issue := range r.issues {
		if containsStr(states, issueStateOf(issue)) {
			list = append(list, issue)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if direction == "DESC" {
			return list[i].createdSeq > list[j].createdSeq
		}
		return list[i].createdSeq < list[j].createdSeq
	})
	if afterNum != nil {
		var filtered []*issueRec
		for _, issue := range list {
			if direction == "DESC" {
				if issue.number < *afterNum {
					filtered = append(filtered, issue)
				}
			} else {
				if issue.number > *afterNum {
					filtered = append(filtered, issue)
				}
			}
		}
		list = filtered
	}
	first := len(list)
	if f, ok := gqlNumber(args["first"]); ok {
		first = int(f)
	}
	if first < 0 {
		first = 0
	}
	if first > len(list) {
		first = len(list)
	}
	page := list[:first]
	var startCursor, endCursor interface{}
	if len(page) > 0 {
		startCursor = cursorOfNumber(page[0].number)
		endCursor = cursorOfNumber(page[len(page)-1].number)
	}
	var nodes []gqlTV
	for _, issue := range page {
		nodes = append(nodes, *issueTypedValue(r, issue))
	}
	return tv("IssueConnection", map[string]interface{}{
		"nodes":      nodes,
		"pageInfo":   pageInfoValue(len(list) > len(page), afterNum != nil, startCursor, endCursor),
		"totalCount": int64(len(list)),
	})
}

func pullsConnection(r *repo, args map[string]interface{}) *gqlTV {
	var states []string
	if arr, ok := args["states"].([]interface{}); ok {
		for _, s := range arr {
			states = append(states, gqlStringOf(s))
		}
	} else {
		states = []string{"OPEN"}
	}
	var afterNum *int
	if afterRaw, ok := args["after"].(string); ok && afterRaw != "" {
		if n, ok := numberFromCursor(afterRaw); ok {
			afterNum = &n
		}
	}
	var list []*pullRec
	for _, pull := range r.pulls {
		if containsStr(states, pullStateOf(pull)) {
			list = append(list, pull)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].createdSeq < list[j].createdSeq })
	if afterNum != nil {
		var filtered []*pullRec
		for _, pull := range list {
			if pull.number > *afterNum {
				filtered = append(filtered, pull)
			}
		}
		list = filtered
	}
	first := len(list)
	if f, ok := gqlNumber(args["first"]); ok {
		first = int(f)
	}
	if first < 0 {
		first = 0
	}
	if first > len(list) {
		first = len(list)
	}
	page := list[:first]
	var startCursor, endCursor interface{}
	if len(page) > 0 {
		startCursor = cursorOfNumber(page[0].number)
		endCursor = cursorOfNumber(page[len(page)-1].number)
	}
	var nodes []gqlTV
	for _, pull := range page {
		nodes = append(nodes, *pullTypedValue(r, pull))
	}
	return tv("PullRequestConnection", map[string]interface{}{
		"nodes":      nodes,
		"pageInfo":   pageInfoValue(len(list) > len(page), afterNum != nil, startCursor, endCursor),
		"totalCount": int64(len(list)),
	})
}

func filesConnection(r *repo, pull *pullRec, args map[string]interface{}) *gqlTV {
	diff := pullDiff(r, pull)
	var paths []string
	if diff != nil {
		for _, f := range diff.files {
			paths = append(paths, f.path)
		}
	}
	var afterPath *string
	if afterRaw, ok := args["after"].(string); ok && afterRaw != "" {
		s := string(lenientB64(afterRaw))
		afterPath = &s
	}
	list := paths
	if afterPath != nil {
		var filtered []string
		for _, p := range list {
			if p > *afterPath {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	first := len(list)
	if f, ok := gqlNumber(args["first"]); ok {
		first = int(f)
	}
	if first < 0 {
		first = 0
	}
	if first > len(list) {
		first = len(list)
	}
	page := list[:first]
	cursorOf := func(p string) string { return base64.StdEncoding.EncodeToString([]byte(p)) }
	var startCursor, endCursor interface{}
	if len(page) > 0 {
		startCursor = cursorOf(page[0])
		endCursor = cursorOf(page[len(page)-1])
	}
	var nodes []gqlTV
	for _, p := range page {
		nodes = append(nodes, *tv("PullRequestChangedFile", map[string]interface{}{"path": p}))
	}
	return tv("PullRequestChangedFileConnection", map[string]interface{}{
		"nodes":      nodes,
		"pageInfo":   pageInfoValue(len(list) > len(page), afterPath != nil, startCursor, endCursor),
		"totalCount": int64(len(list)),
	})
}

// gqlStringOf mirrors JS String(v) for values flowing through GraphQL
// arguments: strings pass through, numbers format JS-style, nil -> "" is
// handled by callers that default first.
func gqlStringOf(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case float64:
		return jsStringOfNumber(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return ""
	}
}

// gqlNumber reports whether v is a GraphQL number argument.
func gqlNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	case float64:
		return t, true
	}
	return 0, false
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func repoFromGlobalID(id string) *repo {
	kind, parts, ok := decodeGlobalID(id)
	if !ok || kind != "Repository" {
		return nil
	}
	dbID, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}
	return reposByID[dbID]
}

func issueFromGlobalID(id string) (*repo, *issueRec, bool) {
	kind, parts, ok := decodeGlobalID(id)
	if !ok || kind != "Issue" || len(parts) < 2 {
		return nil, nil, false
	}
	dbID, err1 := strconv.Atoi(parts[0])
	num, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil, nil, false
	}
	r := reposByID[dbID]
	if r == nil {
		return nil, nil, false
	}
	issue, ok := r.issues[num]
	if !ok {
		return nil, nil, false
	}
	return r, issue, true
}

func pullFromGlobalID(id string) (*repo, *pullRec, bool) {
	kind, parts, ok := decodeGlobalID(id)
	if !ok || kind != "PullRequest" || len(parts) < 2 {
		return nil, nil, false
	}
	dbID, err1 := strconv.Atoi(parts[0])
	num, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil, nil, false
	}
	r := reposByID[dbID]
	if r == nil {
		return nil, nil, false
	}
	pull, ok := r.pulls[num]
	if !ok {
		return nil, nil, false
	}
	return r, pull, true
}

// --- Execution ---

func appendPath(path []JVal, elem JVal) []JVal {
	np := make([]JVal, len(path)+1)
	copy(np, path)
	np[len(path)] = elem
	return np
}

func executeValue(t *gqlTV, selection []*gqlSel, ctx *gqlCtx, path []JVal) JVal {
	if t == nil || t.val == nil {
		return nil
	}
	switch v := t.val.(type) {
	case []gqlTV:
		out := make([]JVal, len(v))
		for i := range v {
			el := v[i]
			out[i] = executeValue(&el, selection, ctx, appendPath(path, int64(i)))
		}
		return out
	case map[string]interface{}:
		if len(selection) == 0 {
			return NewObj()
		}
		return executeSelectionSet(selection, t.typ, v, ctx, path)
	default:
		return t.val
	}
}

func executeSelectionSet(selections []*gqlSel, typeName string, parentValue interface{}, ctx *gqlCtx, path []JVal) *JObj {
	out := NewObj()
	for _, sel := range selections {
		switch sel.kind {
		case "field":
			if skipByDirective(sel.dirs, ctx) {
				continue
			}
			key := sel.name
			if sel.hasAlias {
				key = sel.alias
			}
			fieldPath := appendPath(path, key)
			if sel.name == "__typename" {
				out.Set(key, typeName)
				continue
			}
			table := gqlResolvers[typeName]
			var fn gqlResolver
			if table != nil {
				fn = table[sel.name]
			}
			if fn == nil {
				ctx.errors = append(ctx.errors, NewObj(
					KV("message", "Cannot query field \""+sel.name+"\" on type \""+typeName+"\"."),
					KV("path", fieldPath),
				))
				out.Set(key, nil)
				continue
			}
			args := resolveArgs(sel.args, ctx)
			result, err := fn(parentValue, args, ctx)
			if err != nil {
				if ge, ok := err.(*gqlError); ok {
					ctx.errors = append(ctx.errors, NewObj(
						KV("message", ge.msg),
						KV("path", fieldPath),
					))
					out.Set(key, nil)
					continue
				}
				panic(err)
			}
			if result == nil {
				out.Set(key, nil)
			} else {
				out.Set(key, executeValue(result, sel.selection, ctx, fieldPath))
			}
		case "spread":
			if skipByDirective(sel.dirs, ctx) {
				continue
			}
			frag, ok := ctx.fragments[sel.fragName]
			if !ok {
				pathCopy := make([]JVal, len(path))
				copy(pathCopy, path)
				ctx.errors = append(ctx.errors, NewObj(
					KV("message", "Unknown fragment \""+sel.fragName+"\"."),
					KV("path", pathCopy),
				))
				continue
			}
			if !typeMatches(frag.typeCond, typeName) {
				continue
			}
			mergeInto(out, executeSelectionSet(frag.selection, typeName, parentValue, ctx, path))
		case "inline":
			if skipByDirective(sel.dirs, ctx) {
				continue
			}
			if sel.hasCond && !typeMatches(sel.typeCond, typeName) {
				continue
			}
			mergeInto(out, executeSelectionSet(sel.selection, typeName, parentValue, ctx, path))
		}
	}
	return out
}

func executeGraphQLRequest(req interface{}) *JObj {
	rec, ok := req.(map[string]interface{})
	if !ok {
		return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", "Must provide a query string."))}))
	}
	queryStr, ok := rec["query"].(string)
	if !ok {
		return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", "Must provide a query string."))}))
	}
	variables := map[string]interface{}{}
	if vm, ok := rec["variables"].(map[string]interface{}); ok {
		for k, v := range vm {
			variables[k] = v
		}
	}
	operationName := ""
	hasOpName := false
	if s, ok := rec["operationName"].(string); ok {
		operationName = s
		hasOpName = true
	}
	parser, err := newGqlParser(queryStr)
	if err != nil {
		return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", err.Error()))}))
	}
	definitions, err := parser.parseDocument()
	if err != nil {
		return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", err.Error()))}))
	}
	fragments := map[string]*gqlDefinition{}
	var operations []*gqlDefinition
	for _, def := range definitions {
		if def.kind == "fragment" {
			fragments[def.name] = def
		} else {
			operations = append(operations, def)
		}
	}
	var op *gqlDefinition
	if hasOpName {
		for _, o := range operations {
			if o.name == operationName {
				op = o
				break
			}
		}
		if op == nil {
			return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", "Unknown operation named \""+operationName+"\"."))}))
		}
	} else if len(operations) == 1 {
		op = operations[0]
	} else {
		return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", "Must provide an operation name when there are multiple operations."))}))
	}
	if op.opType == "subscription" {
		return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", "Subscriptions are not supported."))}))
	}
	ctx := &gqlCtx{variables: variables, fragments: fragments}
	for _, vd := range op.varDefs {
		if _, present := ctx.variables[vd.name]; !present {
			if vd.def != nil {
				ctx.variables[vd.name] = valueFromAst(vd.def, ctx.variables)
			} else if vd.typ.kind == "nonNull" {
				return NewObj(KV("data", nil), KV("errors", []JVal{NewObj(KV("message", "Variable $"+vd.name+" of required type was not provided."))}))
			}
		}
	}
	rootType := "Query"
	if op.opType == "mutation" {
		rootType = "Mutation"
	}
	data := executeSelectionSet(op.selection, rootType, map[string]interface{}{}, ctx, nil)
	result := NewObj(KV("data", data))
	if len(ctx.errors) > 0 {
		var errs []JVal
		for _, e := range ctx.errors {
			errs = append(errs, e)
		}
		result.Set("errors", errs)
	}
	return result
}

func handleGraphQL(req *request) response {
	if req.method != "POST" {
		return sendError(404, "Not Found")
	}
	if !isAuthorized(req) {
		return sendError(401, "Bad credentials")
	}
	parsed, err := parseJSONStrict(req.body)
	if err != nil {
		return sendError(400, "Problems parsing JSON")
	}
	if arr, ok := parsed.([]interface{}); ok {
		var results []JVal
		for _, item := range arr {
			results = append(results, executeGraphQLRequest(item))
		}
		if results == nil {
			results = []JVal{}
		}
		return sendJSON(200, results)
	}
	return sendJSON(200, executeGraphQLRequest(parsed))
}
