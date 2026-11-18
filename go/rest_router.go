// REST v3 router: the branch order and every status/message match the
// reference implementation exactly.

package main

import (
	"encoding/base64"
	"sort"
	"strings"
)

func handleRest(req *request, segments []string, path string, q *query) response {
	method := req.method
	if !isAuthorized(req) {
		return sendError(401, "Bad credentials")
	}
	needsBody := method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
	var bodyObj interface{} = map[string]interface{}{}
	if needsBody && len(req.body) > 0 {
		parsed, err := parseJSONStrict(req.body)
		if err != nil {
			return sendError(400, "Problems parsing JSON")
		}
		bodyObj = parsed
	}
	body := asMap(bodyObj)
	if body == nil {
		body = map[string]interface{}{}
	}

	// API root.
	if len(segments) == 0 {
		if method != "GET" {
			return sendError(404, "Not Found")
		}
		return sendJSON(200, NewObj(
			KV("current_user_url", "/api/v3/user"),
			KV("current_user_repositories_url", "/api/v3/user/repos"),
			KV("repository_url", "/api/v3/repos/{owner}/{repo}"),
			KV("user_url", "/api/v3/users/{user}"),
		), headerPair{"x-oauth-scopes", "repo, read:org"})
	}

	// Meta.
	if len(segments) == 1 && segments[0] == "meta" {
		if method != "GET" {
			return sendError(404, "Not Found")
		}
		return sendJSON(200, NewObj(KV("installed_version", "1.0.0")))
	}

	// Authenticated user.
	if len(segments) == 1 && segments[0] == "user" {
		if method != "GET" {
			return sendError(404, "Not Found")
		}
		return sendJSON(200, userJSON())
	}

	// POST /user/repos + GET /user/repos.
	if len(segments) == 2 && segments[0] == "user" && segments[1] == "repos" {
		if method == "POST" {
			name := strings.TrimSpace(asString(body["name"], ""))
			if name == "" {
				return sendError(422, "name is required")
			}
			if _, exists := reposByKey[repoKey(viewerLogin, name)]; exists {
				return sendError(422, "Repository '"+name+"' already exists")
			}
			defBranch := asString(body["default_branch"], "main")
			if defBranch == "" {
				defBranch = "main"
			}
			r := newRepo(name, viewerLogin, asString(body["description"], ""), asBool(body["private"], false), defBranch)
			reposByKey[repoKey(r.owner, r.name)] = r
			reposByID[r.dbID] = r
			return sendJSON(201, repoJSON(r))
		}
		if method == "GET" {
			var all []*repo
			for _, r := range reposByID {
				if r.owner == viewerLogin {
					all = append(all, r)
				}
			}
			sort.Slice(all, func(i, j int) bool { return all[i].dbID < all[j].dbID })
			page, perPage := parsePageParams(q)
			link := pageLinkHeader(req, path, q, page, perPage, len(all))
			var items []JVal
			for _, r := range paginateRepos(all, page, perPage) {
				items = append(items, repoJSON(r))
			}
			if items == nil {
				items = []JVal{}
			}
			return sendJSONExtra(200, items, link)
		}
		return sendError(404, "Not Found")
	}

	// /repos/{owner}/{name}/...
	if len(segments) >= 3 && segments[0] == "repos" {
		owner := segments[1]
		name := segments[2]
		r, ok := reposByKey[repoKey(owner, name)]
		if !ok {
			return sendError(404, "Not Found")
		}
		rest := segments[3:]

		// Repo object itself.
		if len(rest) == 0 {
			if method == "GET" {
				return sendJSON(200, repoJSON(r))
			}
			if method == "PATCH" {
				if v, ok := body["description"].(string); ok {
					r.description = v
				}
				if v, ok := body["private"].(bool); ok {
					r.isPrivate = v
				}
				if v, ok := body["default_branch"].(string); ok && len(v) > 0 {
					r.defaultBranch = v
					bumpVersion(r)
				}
				return sendJSON(200, repoJSON(r))
			}
			if method == "DELETE" {
				delete(reposByKey, repoKey(owner, name))
				delete(reposByID, r.dbID)
				return sendNoContent()
			}
			return sendError(404, "Not Found")
		}

		// git refs
		if rest[0] == "git" && len(rest) > 1 && rest[1] == "refs" {
			refSegments := rest[2:]
			if method == "POST" && len(refSegments) == 0 {
				ref := asString(body["ref"], "")
				sha := asString(body["sha"], "")
				if !strings.HasPrefix(ref, "refs/") || !isOID(sha) {
					return sendError(422, "Invalid ref or sha")
				}
				if _, has := r.objects[sha]; !has {
					return sendError(422, "Invalid ref or sha")
				}
				if _, exists := r.refs[ref]; exists {
					return sendError(422, "Reference already exists")
				}
				r.refs[ref] = sha
				bumpVersion(r)
				return sendJSON(201, refJSON(r, owner, name, ref, sha))
			}
			if method == "GET" && len(refSegments) == 0 {
				var refs []JVal
				for _, rp := range sortedRefList(r) {
					refs = append(refs, refJSON(r, owner, name, rp.name, rp.oid))
				}
				if refs == nil {
					refs = []JVal{}
				}
				return sendJSON(200, refs)
			}
			if method == "GET" {
				refName := "refs/" + strings.Join(refSegments, "/")
				if exact, ok := r.refs[refName]; ok {
					return sendJSON(200, refJSON(r, owner, name, refName, exact))
				}
				var matches []JVal
				for _, rp := range sortedRefList(r) {
					if strings.HasPrefix(rp.name, refName+"/") {
						matches = append(matches, refJSON(r, owner, name, rp.name, rp.oid))
					}
				}
				if len(matches) > 0 {
					return sendJSON(200, matches)
				}
				return sendError(404, "Not Found")
			}
			return sendError(404, "Not Found")
		}

		// branches
		if rest[0] == "branches" {
			branchSegments := rest[1:]
			if method == "GET" && len(branchSegments) == 0 {
				var names []string
				for refName := range r.refs {
					if strings.HasPrefix(refName, "refs/heads/") {
						names = append(names, refName[len("refs/heads/"):])
					}
				}
				sort.Strings(names)
				var out []JVal
				for _, n := range names {
					out = append(out, NewObj(
						KV("name", n),
						KV("commit", NewObj(KV("sha", r.refs["refs/heads/"+n]))),
						KV("protected", false),
					))
				}
				if out == nil {
					out = []JVal{}
				}
				return sendJSON(200, out)
			}
			if method == "GET" {
				branch := strings.Join(branchSegments, "/")
				sha, ok := r.refs["refs/heads/"+branch]
				if !ok {
					return sendError(404, "Not Found")
				}
				return sendJSON(200, NewObj(
					KV("name", branch),
					KV("commit", NewObj(KV("sha", sha))),
					KV("protected", false),
				))
			}
			return sendError(404, "Not Found")
		}

		// commits
		if rest[0] == "commits" {
			if method == "GET" && len(rest) == 1 {
				var tip string
				var hasTip bool
				if shaParam, ok := q.get("sha"); ok {
					tip, hasTip = resolveRef(r, shaParam)
				} else {
					tip, hasTip = headTip(r)
				}
				if !hasTip {
					return sendJSON(200, []JVal{})
				}
				commitOID, ok := peelToCommit(r, tip)
				if !ok {
					return sendJSON(200, []JVal{})
				}
				var order []string
				seen := map[string]bool{}
				stack := []string{commitOID}
				for len(stack) > 0 {
					cur := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if seen[cur] {
						continue
					}
					seen[cur] = true
					order = append(order, cur)
					if view := commitViewOf(r, cur); view != nil {
						for i := len(view.parents) - 1; i >= 0; i-- {
							stack = append(stack, view.parents[i])
						}
					}
				}
				page, perPage := parsePageParams(q)
				link := pageLinkHeader(req, path, q, page, perPage, len(order))
				var items []JVal
				for _, oid := range paginateStrings(order, page, perPage) {
					if obj := commitObject(r, oid); obj != nil {
						items = append(items, obj)
					}
				}
				if items == nil {
					items = []JVal{}
				}
				return sendJSONExtra(200, items, link)
			}
			if method == "GET" && len(rest) == 2 {
				resolved, ok := resolveRef(r, rest[1])
				if !ok {
					return sendError(404, "Not Found")
				}
				commitOID, ok := peelToCommit(r, resolved)
				if !ok {
					return sendError(404, "Not Found")
				}
				obj := commitObject(r, commitOID)
				if obj == nil {
					return sendError(404, "Not Found")
				}
				return sendJSON(200, obj)
			}
			return sendError(404, "Not Found")
		}

		// contents
		if rest[0] == "contents" {
			filePath := strings.Join(rest[1:], "/")
			if method == "GET" {
				var resolved string
				var hasResolved bool
				if refParam, ok := q.get("ref"); ok {
					resolved, hasResolved = resolveRef(r, refParam)
				} else {
					resolved, hasResolved = headTip(r)
				}
				if !hasResolved {
					return sendError(404, "Not Found")
				}
				commitOID, ok := peelToCommit(r, resolved)
				if !ok {
					return sendError(404, "Not Found")
				}
				flat := flatMapOf(r, commitOID)
				if flat == nil {
					return sendError(404, "Not Found")
				}
				if filePath == "" {
					return sendJSON(200, rootListing(r, commitOID, flat))
				}
				if direct, ok := flat[filePath]; ok {
					var bytes []byte
					if obj, ok := r.objects[direct.oid]; ok {
						bytes = obj.data
					}
					return sendJSON(200, NewObj(
						KV("type", "file"),
						KV("encoding", "base64"),
						KV("content", base64.StdEncoding.EncodeToString(bytes)),
						KV("sha", direct.oid),
						KV("size", int64(len(bytes))),
						KV("name", baseName(filePath)),
						KV("path", filePath),
					))
				}
				children := dirListing(r, commitOID, flat, filePath+"/")
				if children == nil {
					return sendError(404, "Not Found")
				}
				return sendJSON(200, children)
			}
			if method == "PUT" {
				if filePath == "" {
					return sendError(404, "Not Found")
				}
				message := asString(body["message"], "")
				contentB64 := asString(body["content"], "")
				branch := asString(body["branch"], r.defaultBranch)
				if branch == "" {
					branch = r.defaultBranch
				}
				var givenSha *string
				if s, ok := body["sha"].(string); ok {
					givenSha = &s
				}
				branchRef := "refs/heads/" + branch
				currentTip, hasTip := r.refs[branchRef]
				if !hasTip && branch != r.defaultBranch {
					currentTip, hasTip = headTip(r)
				}
				exists := false
				var currentOID string
				if hasTip {
					if flat := flatMapOf(r, currentTip); flat != nil {
						if entry, ok := flat[filePath]; ok {
							exists = true
							currentOID = entry.oid
						}
					}
				}
				if givenSha != nil && exists && *givenSha != currentOID {
					return sendError(409, "sha does not match the current file")
				}
				if givenSha != nil && !exists && hasTip {
					return sendError(409, "sha does not match the current file")
				}
				blobBytes := lenientB64(contentB64)
				blobOID := storeObject(r, &gitObject{typ: objBlob, data: blobBytes})
				mode := "100644"
				if exists && hasTip {
					if flat := flatMapOf(r, currentTip); flat != nil {
						if entry, ok := flat[filePath]; ok {
							mode = entry.mode
						}
					}
				}
				commitOID := commitOnBranch(r, branch, message, func(flat map[string]flatEntry) {
					flat[filePath] = flatEntry{mode: mode, oid: blobOID}
				})
				status := 201
				if exists {
					status = 200
				}
				return sendJSON(status, NewObj(
					KV("content", NewObj(
						KV("name", baseName(filePath)),
						KV("path", filePath),
						KV("sha", blobOID),
						KV("size", int64(len(blobBytes))),
						KV("type", "file"),
						KV("encoding", "base64"),
					)),
					KV("commit", NewObj(
						KV("sha", commitOID),
						KV("message", message),
					)),
				))
			}
			if method == "DELETE" {
				if filePath == "" {
					return sendError(404, "Not Found")
				}
				message := asString(body["message"], "")
				branch := asString(body["branch"], r.defaultBranch)
				if branch == "" {
					branch = r.defaultBranch
				}
				var givenSha *string
				if s, ok := body["sha"].(string); ok {
					givenSha = &s
				}
				currentTip, ok := r.refs["refs/heads/"+branch]
				if !ok {
					return sendError(404, "Not Found")
				}
				flat := flatMapOf(r, currentTip)
				var entry flatEntry
				var hasEntry bool
				if flat != nil {
					entry, hasEntry = flat[filePath]
				}
				if !hasEntry {
					return sendError(404, "Not Found")
				}
				if givenSha != nil && *givenSha != entry.oid {
					return sendError(409, "sha does not match the current file")
				}
				commitOID := commitOnBranch(r, branch, message, func(f map[string]flatEntry) {
					delete(f, filePath)
				})
				return sendJSON(200, NewObj(
					KV("content", nil),
					KV("commit", NewObj(
						KV("sha", commitOID),
						KV("message", message),
					)),
				))
			}
			return sendError(404, "Not Found")
		}

		// labels
		if rest[0] == "labels" && len(rest) == 1 {
			if method == "GET" {
				var out []JVal
				for _, name := range r.labelOrder {
					out = append(out, labelJSON(r.labels[name], r))
				}
				if out == nil {
					out = []JVal{}
				}
				return sendJSON(200, out)
			}
			if method == "POST" {
				name := strings.TrimSpace(asString(body["name"], ""))
				if name == "" {
					return sendError(422, "name is required")
				}
				if _, exists := r.labels[name]; exists {
					return sendError(422, "Label already exists")
				}
				label := &labelRec{name: name, color: asString(body["color"], "000000"), description: asString(body["description"], "")}
				r.labels[name] = label
				r.labelOrder = append(r.labelOrder, name)
				return sendJSON(201, labelJSON(label, r))
			}
			return sendError(404, "Not Found")
		}

		// issues
		if rest[0] == "issues" {
			if len(rest) == 1 {
				if method == "POST" {
					title := asString(body["title"], "")
					if title == "" {
						return sendError(422, "title is required")
					}
					labelNames := parseLabelInput(body["labels"])
					for _, n := range labelNames {
						if _, ok := r.labels[n]; !ok {
							return sendError(422, "Label '"+n+"' does not exist")
						}
					}
					issue := createIssueRecord(r, title, asString(body["body"], ""))
					issue.labels = labelNames
					return sendBuffer(201, issueBuffer(r, issue), "application/json")
				}
				if method == "GET" {
					state := q.getOr("state", "open")
					direction := q.getOr("direction", "desc")
					var list []*issueRec
					for _, issue := range r.issues {
						if state != "all" && issue.state != state {
							continue
						}
						list = append(list, issue)
					}
					sort.Slice(list, func(i, j int) bool {
						if direction == "asc" {
							return list[i].createdSeq < list[j].createdSeq
						}
						return list[i].createdSeq > list[j].createdSeq
					})
					page, perPage := parsePageParams(q)
					link := pageLinkHeader(req, path, q, page, perPage, len(list))
					paged := paginateIssues(list, page, perPage)
					var sb strings.Builder
					sb.WriteByte('[')
					for i, issue := range paged {
						if i > 0 {
							sb.WriteByte(',')
						}
						sb.WriteString(issueJSONString(r, issue))
					}
					sb.WriteByte(']')
					return sendBufferExtra(200, []byte(sb.String()), "application/json", link)
				}
				return sendError(404, "Not Found")
			}
			num, ok := jsNumberInt(rest[1])
			if !ok || num < 1 {
				return sendError(404, "Not Found")
			}
			issue, hasIssue := r.issues[num]
			if len(rest) == 2 {
				if !hasIssue {
					return sendError(404, "Not Found")
				}
				if method == "GET" {
					return sendBuffer(200, issueBuffer(r, issue), "application/json")
				}
				if method == "PATCH" {
					if v, ok := body["title"].(string); ok {
						issue.title = v
					}
					if v, ok := body["body"].(string); ok {
						issue.body = v
					}
					if v, ok := body["state"].(string); ok && (v == "open" || v == "closed") {
						issue.state = v
						if v == "closed" {
							closed := fixedISO
							issue.closedAt = &closed
						} else {
							issue.closedAt = nil
						}
					}
					if _, present := body["labels"]; present {
						labelNames := parseLabelInput(body["labels"])
						for _, n := range labelNames {
							if _, ok := r.labels[n]; !ok {
								return sendError(422, "Label '"+n+"' does not exist")
							}
						}
						issue.labels = labelNames
					}
					delete(r.issueBufCache, issue.number)
					return sendBuffer(200, issueBuffer(r, issue), "application/json")
				}
				return sendError(404, "Not Found")
			}
			if len(rest) == 3 && rest[2] == "comments" {
				if !hasIssue {
					return sendError(404, "Not Found")
				}
				if method == "GET" {
					var out []JVal
					for _, c := range issue.comments {
						out = append(out, commentJSON(r, issue, c))
					}
					if out == nil {
						out = []JVal{}
					}
					return sendJSON(200, out)
				}
				if method == "POST" {
					text := asString(body["body"], "")
					if text == "" {
						return sendError(422, "body is required")
					}
					comment := &commentRec{id: nextCommentID, body: text}
					nextCommentID++
					issue.comments = append(issue.comments, comment)
					return sendJSON(201, commentJSON(r, issue, comment))
				}
				return sendError(404, "Not Found")
			}
			return sendError(404, "Not Found")
		}

		// pulls
		if rest[0] == "pulls" {
			if len(rest) == 1 {
				if method == "POST" {
					title := asString(body["title"], "")
					headRaw := asString(body["head"], "")
					baseRaw := asString(body["base"], "")
					if title == "" || headRaw == "" || baseRaw == "" {
						return sendError(422, "title, head and base are required")
					}
					headRef := stripBranchPrefix(headRaw)
					baseRef := stripBranchPrefix(baseRaw)
					if _, ok := r.refs["refs/heads/"+headRef]; !ok {
						return sendError(422, "head branch '"+headRef+"' does not exist")
					}
					if _, ok := r.refs["refs/heads/"+baseRef]; !ok {
						return sendError(422, "base branch '"+baseRef+"' does not exist")
					}
					pull := createPullRecord(r, title, asString(body["body"], ""), headRef, baseRef)
					return sendJSON(201, pullObject(r, pull))
				}
				if method == "GET" {
					state := q.getOr("state", "open")
					var list []*pullRec
					for _, pull := range r.pulls {
						if state != "all" && pull.state != state {
							continue
						}
						list = append(list, pull)
					}
					sort.Slice(list, func(i, j int) bool { return list[i].createdSeq > list[j].createdSeq })
					page, perPage := parsePageParams(q)
					link := pageLinkHeader(req, path, q, page, perPage, len(list))
					var items []JVal
					for _, pull := range paginatePulls(list, page, perPage) {
						items = append(items, pullObject(r, pull))
					}
					if items == nil {
						items = []JVal{}
					}
					return sendJSONExtra(200, items, link)
				}
				return sendError(404, "Not Found")
			}
			num, ok := jsNumberInt(rest[1])
			if !ok || num < 1 {
				return sendError(404, "Not Found")
			}
			pull, hasPull := r.pulls[num]
			if !hasPull {
				return sendError(404, "Not Found")
			}
			if len(rest) == 2 && method == "GET" {
				return sendJSON(200, pullObject(r, pull))
			}
			if len(rest) == 3 && rest[2] == "files" && method == "GET" {
				return sendJSON(200, pullFilesJSON(r, pull))
			}
			if len(rest) == 3 && rest[2] == "merge" && method == "PUT" {
				mergeMethod := asString(body["merge_method"], "squash")
				if mergeMethod != "" && mergeMethod != "squash" {
					return sendError(405, "merge method '"+mergeMethod+"' is not supported")
				}
				if pull.state != "open" {
					return sendError(405, "Pull request is already closed")
				}
				diff := pullDiff(r, pull)
				if diff == nil {
					return sendError(405, "Pull request cannot be merged")
				}
				if diff.conflict {
					return sendError(405, "Merge conflict")
				}
				mergedFlat := map[string]flatEntry{}
				for p, e := range diff.baseFlat {
					mergedFlat[p] = e
				}
				for _, f := range diff.files {
					if f.status == "removed" {
						delete(mergedFlat, f.path)
					} else if headEntry, ok := diff.headFlat[f.path]; ok {
						mergedFlat[f.path] = headEntry
					}
				}
				treeOID := buildTreeFromFlat(r, mergedFlat)
				epoch := fixedEpochSec + r.commitCounter
				r.commitCounter++
				ident := identLine("forge", "forge@make.fun", epoch)
				var parents []string
				if diff.hasBaseTip {
					parents = []string{diff.baseTip}
				}
				sha := storeObject(r, &gitObject{typ: objCommit, data: buildCommitBytes(treeOID, parents, ident, ident, pull.title, "")})
				r.refs["refs/heads/"+pull.baseRef] = sha
				bumpVersion(r)
				pull.state = "closed"
				pull.merged = true
				pull.mergeCommitSha = &sha
				mergedAt := fixedISO
				pull.mergedAt = &mergedAt
				return sendJSON(200, NewObj(
					KV("sha", sha),
					KV("merged", true),
					KV("message", "Pull Request successfully merged"),
				))
			}
			return sendError(404, "Not Found")
		}

		return sendError(404, "Not Found")
	}

	return sendError(404, "Not Found")
}

func refJSON(r *repo, owner, name, refName, sha string) *JObj {
	typ := "commit"
	if obj, ok := r.objects[sha]; ok {
		typ = obj.typ.name()
	}
	return NewObj(
		KV("ref", refName),
		KV("node_id", globalID("Ref", r.dbID, refName)),
		KV("url", "/api/v3/repos/"+owner+"/"+name+"/git/refs/"+refName[5:]),
		KV("object", NewObj(
			KV("sha", sha),
			KV("type", typ),
			KV("url", ""),
		)),
	)
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// jsNumberInt converts a path segment with JS Number() semantics and
// requires an integer result (Number.isInteger).
func jsNumberInt(s string) (int, bool) {
	f, ok := jsParseNumberString(s)
	if !ok {
		return 0, false
	}
	if f != float64(int64(f)) {
		return 0, false
	}
	return int(f), true
}

func paginateRepos(items []*repo, page, perPage int) []*repo {
	start := (page - 1) * perPage
	if start >= len(items) {
		return nil
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func paginateStrings(items []string, page, perPage int) []string {
	start := (page - 1) * perPage
	if start >= len(items) {
		return nil
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func paginateIssues(items []*issueRec, page, perPage int) []*issueRec {
	start := (page - 1) * perPage
	if start >= len(items) {
		return nil
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func paginatePulls(items []*pullRec, page, perPage int) []*pullRec {
	start := (page - 1) * perPage
	if start >= len(items) {
		return nil
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
