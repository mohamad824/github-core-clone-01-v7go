// GraphQL introspection tables (exactly the implemented surface) and
// the field resolvers for every type.

package main

import (
	"sort"
)

type gqlFieldEntry struct {
	name string
	typ  string
}

type typeTableEntry struct {
	kind        string
	fields      []gqlFieldEntry
	enumValues  []string
	inputFields []gqlFieldEntry
}

var typeTable = map[string]*typeTableEntry{
	"Query": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"viewer", "User"}, {"repository", "Repository"}, {"node", "Node"}, {"__type", "__Type"}, {"__schema", "__Schema"},
	}},
	"Mutation": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"createIssue", "CreateIssuePayload"}, {"closeIssue", "CloseIssuePayload"}, {"createPullRequest", "CreatePullRequestPayload"}, {"mergePullRequest", "MergePullRequestPayload"}, {"addComment", "AddCommentPayload"},
	}},
	"Node": {kind: "INTERFACE", fields: []gqlFieldEntry{{"id", "ID"}}},
	"Repository": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"id", "ID"}, {"databaseId", "Int"}, {"name", "String"}, {"nameWithOwner", "String"}, {"owner", "User"},
		{"defaultBranchRef", "Ref"}, {"viewerPermission", "RepositoryPermission"}, {"hasIssuesEnabled", "Boolean"},
		{"issue", "Issue"}, {"issues", "IssueConnection"}, {"pullRequests", "PullRequestConnection"}, {"pullRequest", "PullRequest"},
	}},
	"Issue": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"id", "ID"}, {"number", "Int"}, {"title", "String"}, {"state", "IssueState"}, {"body", "String"},
	}},
	"PullRequest": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"id", "ID"}, {"number", "Int"}, {"headRefName", "String"}, {"baseRefName", "String"},
		{"mergeable", "MergeableState"}, {"state", "PullRequestState"}, {"merged", "Boolean"}, {"files", "PullRequestChangedFileConnection"},
	}},
	"User": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"id", "ID"}, {"login", "String"}, {"name", "String"}, {"email", "String"},
	}},
	"Ref": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"id", "ID"}, {"name", "String"}, {"target", "Commit"},
	}},
	"Commit": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"oid", "String"}, {"message", "String"}, {"abbreviatedOid", "String"},
	}},
	"IssueConnection": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"nodes", "Issue"}, {"pageInfo", "PageInfo"}, {"totalCount", "Int"},
	}},
	"PullRequestConnection": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"nodes", "PullRequest"}, {"pageInfo", "PageInfo"}, {"totalCount", "Int"},
	}},
	"PullRequestChangedFileConnection": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"nodes", "PullRequestChangedFile"}, {"pageInfo", "PageInfo"}, {"totalCount", "Int"},
	}},
	"PullRequestChangedFile": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"path", "String"},
	}},
	"PageInfo": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"hasNextPage", "Boolean"}, {"hasPreviousPage", "Boolean"}, {"startCursor", "String"}, {"endCursor", "String"},
	}},
	"IssueComment": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"id", "ID"}, {"body", "String"},
	}},
	"IssueCommentEdge": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"node", "IssueComment"}, {"cursor", "String"},
	}},
	"CreateIssuePayload": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"issue", "Issue"}, {"clientMutationId", "String"},
	}},
	"CloseIssuePayload": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"issue", "Issue"}, {"clientMutationId", "String"},
	}},
	"CreatePullRequestPayload": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"pullRequest", "PullRequest"}, {"clientMutationId", "String"},
	}},
	"MergePullRequestPayload": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"pullRequest", "PullRequest"}, {"clientMutationId", "String"},
	}},
	"AddCommentPayload": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"commentEdge", "IssueCommentEdge"}, {"clientMutationId", "String"},
	}},
	"IssueState":             {kind: "ENUM", enumValues: []string{"OPEN", "CLOSED"}},
	"PullRequestState":       {kind: "ENUM", enumValues: []string{"OPEN", "CLOSED", "MERGED"}},
	"MergeableState":         {kind: "ENUM", enumValues: []string{"MERGEABLE", "CONFLICTING", "UNKNOWN"}},
	"RepositoryPermission":   {kind: "ENUM", enumValues: []string{"ADMIN", "MAINTAIN", "WRITE", "TRIAGE", "READ"}},
	"OrderDirection":         {kind: "ENUM", enumValues: []string{"ASC", "DESC"}},
	"IssueOrderField":        {kind: "ENUM", enumValues: []string{"CREATED_AT", "UPDATED_AT", "COMMENTS"}},
	"SearchType":             {kind: "ENUM", enumValues: []string{"ISSUE", "REPOSITORY", "USER"}},
	"PullRequestMergeMethod": {kind: "ENUM", enumValues: []string{"MERGE", "SQUASH", "REBASE"}},
	"IssueOrder": {kind: "INPUT_OBJECT", inputFields: []gqlFieldEntry{
		{"field", "IssueOrderField"}, {"direction", "OrderDirection"},
	}},
	"CreateIssueInput": {kind: "INPUT_OBJECT", inputFields: []gqlFieldEntry{
		{"repositoryId", "ID"}, {"title", "String"}, {"body", "String"}, {"clientMutationId", "String"},
	}},
	"CloseIssueInput": {kind: "INPUT_OBJECT", inputFields: []gqlFieldEntry{
		{"issueId", "ID"}, {"clientMutationId", "String"},
	}},
	"CreatePullRequestInput": {kind: "INPUT_OBJECT", inputFields: []gqlFieldEntry{
		{"repositoryId", "ID"}, {"baseRefName", "String"}, {"headRefName", "String"}, {"title", "String"}, {"body", "String"}, {"clientMutationId", "String"},
	}},
	"MergePullRequestInput": {kind: "INPUT_OBJECT", inputFields: []gqlFieldEntry{
		{"pullRequestId", "ID"}, {"mergeMethod", "PullRequestMergeMethod"}, {"clientMutationId", "String"},
	}},
	"AddCommentInput": {kind: "INPUT_OBJECT", inputFields: []gqlFieldEntry{
		{"subjectId", "ID"}, {"body", "String"}, {"clientMutationId", "String"},
	}},
	"String":  {kind: "SCALAR"},
	"Int":     {kind: "SCALAR"},
	"Float":   {kind: "SCALAR"},
	"Boolean": {kind: "SCALAR"},
	"ID":      {kind: "SCALAR"},
	"__Type": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"name", "String"}, {"kind", "String"}, {"description", "String"}, {"fields", "__Field"},
		{"enumValues", "__EnumValue"}, {"inputFields", "__InputValue"}, {"interfaces", "__Type"},
		{"possibleTypes", "__Type"}, {"specifiedByURL", "String"},
	}},
	"__Field": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"name", "String"}, {"description", "String"}, {"args", "__InputValue"}, {"type", "__Type"},
		{"isDeprecated", "Boolean"}, {"deprecationReason", "String"},
	}},
	"__EnumValue": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"name", "String"}, {"description", "String"}, {"isDeprecated", "Boolean"}, {"deprecationReason", "String"},
	}},
	"__InputValue": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"name", "String"}, {"description", "String"}, {"type", "__Type"}, {"defaultValue", "String"},
	}},
	"__Schema": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"queryType", "__Type"}, {"mutationType", "__Type"}, {"subscriptionType", "__Type"},
		{"types", "__Type"}, {"directives", "__Directive"},
	}},
	"__Directive": {kind: "OBJECT", fields: []gqlFieldEntry{
		{"name", "String"}, {"description", "String"}, {"locations", "String"}, {"args", "__InputValue"},
	}},
}

var sortedTypeNames []string

func init() {
	for name := range typeTable {
		sortedTypeNames = append(sortedTypeNames, name)
	}
	sort.Strings(sortedTypeNames)
}

func typeWrapper(name string) *gqlTV {
	return tv("__Type", map[string]interface{}{"typeName": name})
}

func payloadMap(parent interface{}) map[string]interface{} {
	if m, ok := parent.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func payloadRepo(parent interface{}) *repo {
	if r, ok := payloadMap(parent)["repo"].(*repo); ok {
		return r
	}
	return nil
}

func payloadIssue(parent interface{}) *issueRec {
	if i, ok := payloadMap(parent)["issue"].(*issueRec); ok {
		return i
	}
	return nil
}

func payloadPull(parent interface{}) *pullRec {
	if p, ok := payloadMap(parent)["pull"].(*pullRec); ok {
		return p
	}
	return nil
}

// numberArg mirrors JS Number(v) for the `number` argument, requiring an
// integer result (a non-integer or NaN matches nothing).
func numberArg(v interface{}) (int, bool) {
	f, ok := jsNumber(v)
	if !ok {
		return 0, false
	}
	if f != float64(int64(f)) {
		return 0, false
	}
	return int(f), true
}

func inputMap(args map[string]interface{}) map[string]interface{} {
	if m, ok := args["input"].(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func clientMutationID(input map[string]interface{}) interface{} {
	if s, ok := input["clientMutationId"].(string); ok {
		return s
	}
	return nil
}

var gqlResolvers = map[string]map[string]gqlResolver{
	"Query": {
		"viewer": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return userTypedValue(), nil
		},
		"repository": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			owner := gqlStringOf(args["owner"])
			name := gqlStringOf(args["name"])
			r, ok := reposByKey[repoKey(owner, name)]
			if !ok {
				return nil, nil
			}
			return repoTypedValue(r), nil
		},
		"node": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			id := gqlStringOf(args["id"])
			kind, parts, ok := decodeGlobalID(id)
			if !ok {
				return nil, nil
			}
			switch kind {
			case "Repository":
				r := repoFromGlobalID(id)
				if r == nil {
					return nil, nil
				}
				return repoTypedValue(r), nil
			case "Issue":
				r, issue, ok := issueFromGlobalID(id)
				if !ok {
					return nil, nil
				}
				return issueTypedValue(r, issue), nil
			case "PullRequest":
				r, pull, ok := pullFromGlobalID(id)
				if !ok {
					return nil, nil
				}
				return pullTypedValue(r, pull), nil
			case "User":
				uid, ok := numberArg(parts[0])
				if !ok {
					return nil, nil
				}
				if uid == scen.viewer.id {
					return userTypedValue(), nil
				}
				return nil, nil
			case "IssueComment":
				if len(parts) < 2 {
					return nil, nil
				}
				dbID, ok1 := numberArg(parts[0])
				cid, ok2 := numberArg(parts[1])
				if !ok1 || !ok2 {
					return nil, nil
				}
				r := reposByID[dbID]
				if r == nil {
					return nil, nil
				}
				for _, issue := range r.issues {
					for _, comment := range issue.comments {
						if comment.id == cid {
							return tv("IssueComment", map[string]interface{}{"repo": r, "issue": issue, "comment": comment}), nil
						}
					}
				}
				return nil, nil
			}
			return nil, nil
		},
		"__type": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			name := gqlStringOf(args["name"])
			if typeTable[name] == nil {
				return nil, nil
			}
			return typeWrapper(name), nil
		},
		"__schema": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("__Schema", map[string]interface{}{}), nil
		},
	},
	"Mutation": {
		"createIssue": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			input := inputMap(args)
			var r *repo
			if id, ok := input["repositoryId"].(string); ok {
				r = repoFromGlobalID(id)
			}
			if r == nil {
				return nil, &gqlError{"Repository not found"}
			}
			title := gqlStringOf(input["title"])
			if title == "" {
				return nil, &gqlError{"title is required"}
			}
			issue := createIssueRecord(r, title, gqlStringOf(input["body"]))
			return tv("CreateIssuePayload", map[string]interface{}{
				"issue": issueTypedValue(r, issue), "clientMutationId": clientMutationID(input),
			}), nil
		},
		"closeIssue": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			input := inputMap(args)
			var foundR *repo
			var foundIssue *issueRec
			var found bool
			if id, ok := input["issueId"].(string); ok {
				foundR, foundIssue, found = issueFromGlobalID(id)
			}
			if !found {
				return nil, &gqlError{"Issue not found"}
			}
			foundIssue.state = "closed"
			closed := fixedISO
			foundIssue.closedAt = &closed
			delete(foundR.issueBufCache, foundIssue.number)
			return tv("CloseIssuePayload", map[string]interface{}{
				"issue": issueTypedValue(foundR, foundIssue), "clientMutationId": clientMutationID(input),
			}), nil
		},
		"createPullRequest": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			input := inputMap(args)
			var r *repo
			if id, ok := input["repositoryId"].(string); ok {
				r = repoFromGlobalID(id)
			}
			if r == nil {
				return nil, &gqlError{"Repository not found"}
			}
			headRef := stripBranchPrefix(gqlStringOf(input["headRefName"]))
			baseRef := stripBranchPrefix(gqlStringOf(input["baseRefName"]))
			if _, ok := r.refs["refs/heads/"+headRef]; !ok {
				return nil, &gqlError{"head branch '" + headRef + "' does not exist"}
			}
			if _, ok := r.refs["refs/heads/"+baseRef]; !ok {
				return nil, &gqlError{"base branch '" + baseRef + "' does not exist"}
			}
			pull := createPullRecord(r, gqlStringOf(input["title"]), gqlStringOf(input["body"]), headRef, baseRef)
			return tv("CreatePullRequestPayload", map[string]interface{}{
				"pullRequest": pullTypedValue(r, pull), "clientMutationId": clientMutationID(input),
			}), nil
		},
		"mergePullRequest": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			input := inputMap(args)
			var foundR *repo
			var foundPull *pullRec
			var found bool
			if id, ok := input["pullRequestId"].(string); ok {
				foundR, foundPull, found = pullFromGlobalID(id)
			}
			if !found {
				return nil, &gqlError{"Pull request not found"}
			}
			if _, err := squashMergePull(foundR, foundPull); err != nil {
				return nil, err
			}
			return tv("MergePullRequestPayload", map[string]interface{}{
				"pullRequest": pullTypedValue(foundR, foundPull), "clientMutationId": clientMutationID(input),
			}), nil
		},
		"addComment": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			input := inputMap(args)
			subjectID := gqlStringOf(input["subjectId"])
			r, issue, found := issueFromGlobalID(subjectID)
			if !found {
				return nil, &gqlError{"Subject must be an issue"}
			}
			comment := &commentRec{id: nextCommentID, body: gqlStringOf(input["body"])}
			nextCommentID++
			issue.comments = append(issue.comments, comment)
			return tv("AddCommentPayload", map[string]interface{}{
				"commentEdge": tv("IssueCommentEdge", map[string]interface{}{
					"node":   tv("IssueComment", map[string]interface{}{"repo": r, "issue": issue, "comment": comment}),
					"cursor": cursorOfNumber(comment.id),
				}),
				"clientMutationId": clientMutationID(input),
			}), nil
		},
	},
	"Repository": {
		"id": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("ID", globalID("Repository", payloadRepo(parent).dbID)), nil
		},
		"databaseId": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Int", int64(payloadRepo(parent).dbID)), nil
		},
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadRepo(parent).name), nil
		},
		"nameWithOwner": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r := payloadRepo(parent)
			return tv("String", r.owner+"/"+r.name), nil
		},
		"owner": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return userTypedValue(), nil
		},
		"defaultBranchRef": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r := payloadRepo(parent)
			return tv("Ref", map[string]interface{}{"repo": r, "name": r.defaultBranch}), nil
		},
		"viewerPermission": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("RepositoryPermission", "ADMIN"), nil
		},
		"hasIssuesEnabled": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Boolean", true), nil
		},
		"issue": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r := payloadRepo(parent)
			num, ok := numberArg(args["number"])
			if !ok {
				return nil, nil
			}
			issue, ok := r.issues[num]
			if !ok {
				return nil, nil
			}
			return issueTypedValue(r, issue), nil
		},
		"issues": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return issuesConnection(payloadRepo(parent), args), nil
		},
		"pullRequests": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return pullsConnection(payloadRepo(parent), args), nil
		},
		"pullRequest": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r := payloadRepo(parent)
			num, ok := numberArg(args["number"])
			if !ok {
				return nil, nil
			}
			pull, ok := r.pulls[num]
			if !ok {
				return nil, nil
			}
			return pullTypedValue(r, pull), nil
		},
	},
	"Issue": {
		"id": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r, issue := payloadRepo(parent), payloadIssue(parent)
			return tv("ID", globalID("Issue", r.dbID, issue.number)), nil
		},
		"number": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Int", int64(payloadIssue(parent).number)), nil
		},
		"title": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadIssue(parent).title), nil
		},
		"state": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("IssueState", issueStateOf(payloadIssue(parent))), nil
		},
		"body": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadIssue(parent).body), nil
		},
	},
	"PullRequest": {
		"id": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r, pull := payloadRepo(parent), payloadPull(parent)
			return tv("ID", globalID("PullRequest", r.dbID, pull.number)), nil
		},
		"number": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Int", int64(payloadPull(parent).number)), nil
		},
		"headRefName": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadPull(parent).headRef), nil
		},
		"baseRefName": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadPull(parent).baseRef), nil
		},
		"mergeable": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			r, pull := payloadRepo(parent), payloadPull(parent)
			if pull.state != "open" {
				return tv("MergeableState", "UNKNOWN"), nil
			}
			diff := pullDiff(r, pull)
			if diff == nil {
				return tv("MergeableState", "UNKNOWN"), nil
			}
			if diff.conflict {
				return tv("MergeableState", "CONFLICTING"), nil
			}
			return tv("MergeableState", "MERGEABLE"), nil
		},
		"state": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("PullRequestState", pullStateOf(payloadPull(parent))), nil
		},
		"merged": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Boolean", payloadPull(parent).merged), nil
		},
		"files": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return filesConnection(payloadRepo(parent), payloadPull(parent), args), nil
		},
	},
	"User": {
		"id": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			id := payloadMap(parent)["id"].(int)
			return tv("ID", globalID("User", id)), nil
		},
		"login": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["login"].(string)), nil
		},
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["name"].(string)), nil
		},
		"email": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["email"].(string)), nil
		},
	},
	"Ref": {
		"id": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			m := payloadMap(parent)
			r := m["repo"].(*repo)
			return tv("ID", globalID("Ref", r.dbID, "refs/heads/"+m["name"].(string))), nil
		},
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["name"].(string)), nil
		},
		"target": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			m := payloadMap(parent)
			r := m["repo"].(*repo)
			name := m["name"].(string)
			tip, ok := r.refs["refs/heads/"+name]
			if !ok {
				return nil, nil
			}
			commitOID, ok := peelToCommit(r, tip)
			if !ok {
				return nil, nil
			}
			return tv("Commit", map[string]interface{}{"repo": r, "oid": commitOID}), nil
		},
	},
	"Commit": {
		"oid": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["oid"].(string)), nil
		},
		"message": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			m := payloadMap(parent)
			r := m["repo"].(*repo)
			oid := m["oid"].(string)
			view := commitViewOf(r, oid)
			msg := ""
			if view != nil {
				msg = view.message
			}
			return tv("String", msg), nil
		},
		"abbreviatedOid": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			oid := payloadMap(parent)["oid"].(string)
			return tv("String", oid[:7]), nil
		},
	},
	"IssueConnection": {
		"nodes": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Issue", payloadMap(parent)["nodes"].([]gqlTV)), nil
		},
		"pageInfo": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("PageInfo", payloadMap(parent)["pageInfo"].(map[string]interface{})), nil
		},
		"totalCount": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Int", payloadMap(parent)["totalCount"].(int64)), nil
		},
	},
	"PullRequestConnection": {
		"nodes": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("PullRequest", payloadMap(parent)["nodes"].([]gqlTV)), nil
		},
		"pageInfo": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("PageInfo", payloadMap(parent)["pageInfo"].(map[string]interface{})), nil
		},
		"totalCount": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Int", payloadMap(parent)["totalCount"].(int64)), nil
		},
	},
	"PullRequestChangedFileConnection": {
		"nodes": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("PullRequestChangedFile", payloadMap(parent)["nodes"].([]gqlTV)), nil
		},
		"pageInfo": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("PageInfo", payloadMap(parent)["pageInfo"].(map[string]interface{})), nil
		},
		"totalCount": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Int", payloadMap(parent)["totalCount"].(int64)), nil
		},
	},
	"PullRequestChangedFile": {
		"path": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["path"].(string)), nil
		},
	},
	"PageInfo": {
		"hasNextPage": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Boolean", payloadMap(parent)["hasNextPage"].(bool)), nil
		},
		"hasPreviousPage": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Boolean", payloadMap(parent)["hasPreviousPage"].(bool)), nil
		},
		"startCursor": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["startCursor"]), nil
		},
		"endCursor": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["endCursor"]), nil
		},
	},
	"IssueComment": {
		"id": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			m := payloadMap(parent)
			r := m["repo"].(*repo)
			comment := m["comment"].(*commentRec)
			return tv("ID", globalID("IssueComment", r.dbID, comment.id)), nil
		},
		"body": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["comment"].(*commentRec).body), nil
		},
	},
	"IssueCommentEdge": {
		"node": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return payloadMap(parent)["node"].(*gqlTV), nil
		},
		"cursor": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["cursor"].(string)), nil
		},
	},
	"CreateIssuePayload": {
		"issue": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return payloadMap(parent)["issue"].(*gqlTV), nil
		},
		"clientMutationId": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["clientMutationId"]), nil
		},
	},
	"CloseIssuePayload": {
		"issue": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return payloadMap(parent)["issue"].(*gqlTV), nil
		},
		"clientMutationId": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["clientMutationId"]), nil
		},
	},
	"CreatePullRequestPayload": {
		"pullRequest": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return payloadMap(parent)["pullRequest"].(*gqlTV), nil
		},
		"clientMutationId": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["clientMutationId"]), nil
		},
	},
	"MergePullRequestPayload": {
		"pullRequest": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return payloadMap(parent)["pullRequest"].(*gqlTV), nil
		},
		"clientMutationId": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["clientMutationId"]), nil
		},
	},
	"AddCommentPayload": {
		"commentEdge": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return payloadMap(parent)["commentEdge"].(*gqlTV), nil
		},
		"clientMutationId": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["clientMutationId"]), nil
		},
	},
	"__Type": {
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["typeName"].(string)), nil
		},
		"kind": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			entry := typeTable[payloadMap(parent)["typeName"].(string)]
			kind := "SCALAR"
			if entry != nil {
				kind = entry.kind
			}
			return tv("String", kind), nil
		},
		"description": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
		"fields": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			entry := typeTable[payloadMap(parent)["typeName"].(string)]
			if entry == nil || entry.fields == nil {
				return nil, nil
			}
			var nodes []gqlTV
			for _, f := range entry.fields {
				nodes = append(nodes, *tv("__Field", map[string]interface{}{"name": f.name, "typeName": f.typ}))
			}
			return tv("__Field", nodes), nil
		},
		"enumValues": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			entry := typeTable[payloadMap(parent)["typeName"].(string)]
			if entry == nil || entry.enumValues == nil {
				return nil, nil
			}
			var nodes []gqlTV
			for _, n := range entry.enumValues {
				nodes = append(nodes, *tv("__EnumValue", map[string]interface{}{"name": n}))
			}
			return tv("__EnumValue", nodes), nil
		},
		"inputFields": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			entry := typeTable[payloadMap(parent)["typeName"].(string)]
			if entry == nil || entry.inputFields == nil {
				return nil, nil
			}
			var nodes []gqlTV
			for _, f := range entry.inputFields {
				nodes = append(nodes, *tv("__InputValue", map[string]interface{}{"name": f.name, "typeName": f.typ}))
			}
			return tv("__InputValue", nodes), nil
		},
		"interfaces": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("__Type", []gqlTV{}), nil
		},
		"possibleTypes": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			name := payloadMap(parent)["typeName"].(string)
			if name == "Node" {
				var nodes []gqlTV
				for _, n := range nodeTypes {
					nodes = append(nodes, *typeWrapper(n))
				}
				return tv("__Type", nodes), nil
			}
			return tv("__Type", []gqlTV{}), nil
		},
		"specifiedByURL": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
	},
	"__Field": {
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["name"].(string)), nil
		},
		"description": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
		"args": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("__InputValue", []gqlTV{}), nil
		},
		"type": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return typeWrapper(payloadMap(parent)["typeName"].(string)), nil
		},
		"isDeprecated": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Boolean", false), nil
		},
		"deprecationReason": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
	},
	"__EnumValue": {
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["name"].(string)), nil
		},
		"description": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
		"isDeprecated": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("Boolean", false), nil
		},
		"deprecationReason": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
	},
	"__InputValue": {
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["name"].(string)), nil
		},
		"description": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
		"type": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return typeWrapper(payloadMap(parent)["typeName"].(string)), nil
		},
		"defaultValue": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
	},
	"__Schema": {
		"queryType": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return typeWrapper("Query"), nil
		},
		"mutationType": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return typeWrapper("Mutation"), nil
		},
		"subscriptionType": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return nil, nil
		},
		"types": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			var nodes []gqlTV
			for _, n := range sortedTypeNames {
				nodes = append(nodes, *typeWrapper(n))
			}
			return tv("__Type", nodes), nil
		},
		"directives": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("__Directive", []gqlTV{}), nil
		},
	},
	"__Directive": {
		"name": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", payloadMap(parent)["name"].(string)), nil
		},
		"description": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", nil), nil
		},
		"locations": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("String", []gqlTV{}), nil
		},
		"args": func(parent interface{}, args map[string]interface{}, ctx *gqlCtx) (*gqlTV, error) {
			return tv("__InputValue", []gqlTV{}), nil
		},
	},
}
