// REST v3: JSON views, pull-request diffs, pagination, and the router.
// Key order in every view matches the reference implementation exactly.

package main

import (
	"sort"
	"strconv"
	"strings"
)

// --- Shared record helpers (REST, GraphQL, and MCP act on one store) ---

func createIssueRecord(r *repo, title, bodyText string) *issueRec {
	r.seq++
	id := nextObjectID
	nextObjectID++
	issue := &issueRec{
		id:         id,
		number:     r.seq,
		title:      title,
		body:       bodyText,
		state:      "open",
		labels:     []string{},
		createdSeq: r.seq,
	}
	r.issues[issue.number] = issue
	return issue
}

func createPullRecord(r *repo, title, bodyText, headRef, baseRef string) *pullRec {
	r.seq++
	id := nextObjectID
	nextObjectID++
	pull := &pullRec{
		id:         id,
		number:     r.seq,
		title:      title,
		body:       bodyText,
		headRef:    headRef,
		baseRef:    baseRef,
		state:      "open",
		createdSeq: r.seq,
	}
	r.pulls[pull.number] = pull
	return pull
}

type gqlError struct{ msg string }

func (e *gqlError) Error() string { return e.msg }

// squashMergePull performs the shared squash merge. Returns the merge
// commit sha or a *gqlError describing why the merge cannot happen.
func squashMergePull(r *repo, pull *pullRec) (string, error) {
	if pull.state != "open" {
		return "", &gqlError{"Pull request is already closed"}
	}
	diff := pullDiff(r, pull)
	if diff == nil {
		return "", &gqlError{"Pull request cannot be merged"}
	}
	if diff.conflict {
		return "", &gqlError{"Merge conflict"}
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
	if diff.baseTip != "" {
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
	return sha, nil
}

// --- JSON views ---

var cachedUserJSONString = ""

func userJSON() *JObj {
	return NewObj(
		KV("login", viewerLogin),
		KV("id", int64(scen.viewer.id)),
		KV("node_id", globalID("User", scen.viewer.id)),
		KV("name", scen.viewer.name),
		KV("email", scen.viewer.email),
		KV("type", "User"),
		KV("site_admin", false),
	)
}

func userJSONString() string {
	if cachedUserJSONString == "" {
		cachedUserJSONString = string(MarshalJSON(userJSON()))
	}
	return cachedUserJSONString
}

func repoJSON(r *repo) *JObj {
	return NewObj(
		KV("id", int64(r.dbID)),
		KV("node_id", globalID("Repository", r.dbID)),
		KV("name", r.name),
		KV("full_name", r.owner+"/"+r.name),
		KV("owner", NewObj(
			KV("login", r.owner),
			KV("id", int64(scen.viewer.id)),
			KV("type", "User"),
		)),
		KV("private", r.isPrivate),
		KV("description", r.description),
		KV("default_branch", r.defaultBranch),
		KV("has_issues", true),
		KV("has_projects", false),
		KV("has_wiki", false),
		KV("created_at", fixedISO),
		KV("updated_at", fixedISO),
		KV("pushed_at", fixedISO),
		KV("url", "/api/v3/repos/"+r.owner+"/"+r.name),
		KV("html_url", "https://example.invalid/"+r.owner+"/"+r.name),
	)
}

func labelIDOf(r *repo, name string) int {
	for i, key := range r.labelOrder {
		if key == name {
			return i + 1
		}
	}
	return len(r.labelOrder) + 1
}

func labelJSON(label *labelRec, r *repo) *JObj {
	return NewObj(
		KV("id", int64(labelIDOf(r, label.name))),
		KV("node_id", globalID("Label", r.dbID, label.name)),
		KV("name", label.name),
		KV("color", label.color),
		KV("description", label.description),
		KV("url", "/api/v3/repos/"+r.owner+"/"+r.name+"/labels/"+encodeURIComponent(label.name)),
	)
}

func labelJSONString(label *labelRec, r *repo) string {
	return `{"id":` + strconv.Itoa(labelIDOf(r, label.name)) +
		`,"node_id":` + JSONString(globalID("Label", r.dbID, label.name)) +
		`,"name":` + JSONString(label.name) +
		`,"color":` + JSONString(label.color) +
		`,"description":` + JSONString(label.description) +
		`,"url":` + JSONString("/api/v3/repos/"+r.owner+"/"+r.name+"/labels/"+encodeURIComponent(label.name)) +
		`}`
}

// encodeURIComponent mirrors JS encodeURIComponent: everything except
// A-Z a-z 0-9 - _ . ! ~ * ' ( ) is percent-encoded (uppercase hex).
func encodeURIComponent(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '!' || c == '~' || c == '*' || c == '\'' || c == '(' || c == ')' {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('%')
			sb.WriteByte("0123456789ABCDEF"[c>>4])
			sb.WriteByte("0123456789ABCDEF"[c&0x0f])
		}
	}
	return sb.String()
}

func issueObject(r *repo, issue *issueRec) *JObj {
	var labels []JVal
	for _, name := range issue.labels {
		if label, ok := r.labels[name]; ok {
			labels = append(labels, labelJSON(label, r))
		} else {
			labels = append(labels, NewObj(
				KV("name", name),
				KV("color", "000000"),
				KV("description", ""),
			))
		}
	}
	if labels == nil {
		labels = []JVal{}
	}
	var closedAt JVal
	if issue.closedAt != nil {
		closedAt = *issue.closedAt
	}
	return NewObj(
		KV("id", int64(issue.id)),
		KV("node_id", globalID("Issue", r.dbID, issue.number)),
		KV("number", int64(issue.number)),
		KV("title", issue.title),
		KV("body", issue.body),
		KV("state", issue.state),
		KV("user", userJSON()),
		KV("labels", labels),
		KV("created_at", fixedISO),
		KV("updated_at", fixedISO),
		KV("closed_at", closedAt),
		KV("url", "/api/v3/repos/"+r.owner+"/"+r.name+"/issues/"+strconv.Itoa(issue.number)),
		KV("html_url", "https://example.invalid/"+r.owner+"/"+r.name+"/issues/"+strconv.Itoa(issue.number)),
		KV("comments_url", "/api/v3/repos/"+r.owner+"/"+r.name+"/issues/"+strconv.Itoa(issue.number)+"/comments"),
	)
}

// issueJSONString is the hand-rolled twin of MarshalJSON(issueObject):
// same key order, same values, every dynamic string escaped by the same
// JSON.stringify-compatible escaper. A unit test pins the two together.
func issueJSONString(r *repo, issue *issueRec) string {
	var sb strings.Builder
	sb.WriteString(`{"id":`)
	sb.WriteString(strconv.Itoa(issue.id))
	sb.WriteString(`,"node_id":`)
	sb.WriteString(JSONString(globalID("Issue", r.dbID, issue.number)))
	sb.WriteString(`,"number":`)
	sb.WriteString(strconv.Itoa(issue.number))
	sb.WriteString(`,"title":`)
	sb.WriteString(JSONString(issue.title))
	sb.WriteString(`,"body":`)
	sb.WriteString(JSONString(issue.body))
	sb.WriteString(`,"state":`)
	sb.WriteString(JSONString(issue.state))
	sb.WriteString(`,"user":`)
	sb.WriteString(userJSONString())
	sb.WriteString(`,"labels":[`)
	for i, name := range issue.labels {
		if i > 0 {
			sb.WriteByte(',')
		}
		if label, ok := r.labels[name]; ok {
			sb.WriteString(labelJSONString(label, r))
		} else {
			sb.WriteString(`{"name":` + JSONString(name) + `,"color":"000000","description":""}`)
		}
	}
	sb.WriteString(`],"created_at":`)
	sb.WriteString(fixedISOJSON)
	sb.WriteString(`,"updated_at":`)
	sb.WriteString(fixedISOJSON)
	sb.WriteString(`,"closed_at":`)
	if issue.closedAt == nil {
		sb.WriteString("null")
	} else {
		sb.WriteString(JSONString(*issue.closedAt))
	}
	base := "/api/v3/repos/" + r.owner + "/" + r.name + "/issues/" + strconv.Itoa(issue.number)
	sb.WriteString(`,"url":`)
	sb.WriteString(JSONString(base))
	sb.WriteString(`,"html_url":`)
	sb.WriteString(JSONString("https://example.invalid/" + r.owner + "/" + r.name + "/issues/" + strconv.Itoa(issue.number)))
	sb.WriteString(`,"comments_url":`)
	sb.WriteString(JSONString(base + "/comments"))
	sb.WriteString(`}`)
	return sb.String()
}

func issueBuffer(r *repo, issue *issueRec) []byte {
	if cached, ok := r.issueBufCache[issue.number]; ok {
		return cached
	}
	buf := []byte(issueJSONString(r, issue))
	r.issueBufCache[issue.number] = buf
	return buf
}

func commitObject(r *repo, oid string) *JObj {
	view := commitViewOf(r, oid)
	if view == nil {
		return nil
	}
	authorName, authorEmail, authorDate := splitIdent(view.author)
	committerName, committerEmail, committerDate := splitIdent(view.committer)
	var authorUser JVal
	if authorEmail == scen.viewer.email {
		authorUser = userJSON()
	}
	var committerUser JVal
	if committerEmail == scen.viewer.email {
		committerUser = userJSON()
	}
	var parents []JVal
	for _, p := range view.parents {
		parents = append(parents, NewObj(KV("sha", p)))
	}
	if parents == nil {
		parents = []JVal{}
	}
	return NewObj(
		KV("sha", oid),
		KV("node_id", globalID("Commit", r.dbID, oid)),
		KV("commit", NewObj(
			KV("message", view.message),
			KV("author", NewObj(
				KV("name", authorName),
				KV("email", authorEmail),
				KV("date", authorDate),
			)),
			KV("committer", NewObj(
				KV("name", committerName),
				KV("email", committerEmail),
				KV("date", committerDate),
			)),
			KV("tree", NewObj(KV("sha", view.tree))),
		)),
		KV("parents", parents),
		KV("author", authorUser),
		KV("committer", committerUser),
		KV("url", "/api/v3/repos/"+r.owner+"/"+r.name+"/commits/"+oid),
		KV("html_url", "https://example.invalid/"+r.owner+"/"+r.name+"/commit/"+oid),
	)
}

// --- Pull-request diffs ---

type diffEntry struct {
	path   string
	status string // "added" | "modified" | "removed"
	oldOID *string
	newOID *string
}

func diffFlats(base, head map[string]flatEntry) []diffEntry {
	var out []diffEntry
	for path, entry := range head {
		b, ok := base[path]
		if !ok {
			oid := entry.oid
			out = append(out, diffEntry{path: path, status: "added", newOID: &oid})
		} else if b.oid != entry.oid || b.mode != entry.mode {
			oldOID, newOID := b.oid, entry.oid
			out = append(out, diffEntry{path: path, status: "modified", oldOID: &oldOID, newOID: &newOID})
		}
	}
	for path, entry := range base {
		if _, ok := head[path]; !ok {
			oid := entry.oid
			out = append(out, diffEntry{path: path, status: "removed", oldOID: &oid})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func lineCountOf(r *repo, oid *string) int {
	if oid == nil {
		return 0
	}
	obj, ok := r.objects[*oid]
	if !ok || obj.typ != objBlob {
		return 0
	}
	if len(obj.data) == 0 {
		return 0
	}
	n := 0
	for _, b := range obj.data {
		if b == '\n' {
			n++
		}
	}
	if obj.data[len(obj.data)-1] != '\n' {
		n++
	}
	return n
}

type pullDiffResult struct {
	baseTip       string
	hasBaseTip    bool
	headTip       string
	hasHeadTip    bool
	baseFlat      map[string]flatEntry
	headFlat      map[string]flatEntry
	mergeBaseFlat map[string]flatEntry
	files         []diffEntry
	conflict      bool
}

func pullDiff(r *repo, pull *pullRec) *pullDiffResult {
	baseTip, okB := resolveRef(r, pull.baseRef)
	headTipOID, okH := resolveRef(r, pull.headRef)
	if !okB || !okH {
		return nil
	}
	baseFlat := flatMapOf(r, baseTip)
	if baseFlat == nil {
		baseFlat = map[string]flatEntry{}
	}
	headFlat := flatMapOf(r, headTipOID)
	if headFlat == nil {
		headFlat = map[string]flatEntry{}
	}
	mergeBaseFlat := map[string]flatEntry{}
	if mb, ok := mergeBase(r, baseTip, headTipOID); ok {
		if f := flatMapOf(r, mb); f != nil {
			mergeBaseFlat = f
		}
	}
	files := diffFlats(mergeBaseFlat, headFlat)
	conflict := false
	allPaths := map[string]bool{}
	for p := range mergeBaseFlat {
		allPaths[p] = true
	}
	for p := range baseFlat {
		allPaths[p] = true
	}
	for p := range headFlat {
		allPaths[p] = true
	}
	for path := range allPaths {
		m, hasM := mergeBaseFlat[path]
		b, hasB := baseFlat[path]
		h, hasH := headFlat[path]
		var mOID, mMode, bOID, bMode, hOID, hMode *string
		if hasM {
			mOID, mMode = &m.oid, &m.mode
		}
		if hasB {
			bOID, bMode = &b.oid, &b.mode
		}
		if hasH {
			hOID, hMode = &h.oid, &h.mode
		}
		headChanged := !strPtrEq(mOID, hOID) || !strPtrEq(mMode, hMode)
		baseChanged := !strPtrEq(mOID, bOID) || !strPtrEq(mMode, bMode)
		if headChanged && baseChanged {
			var headEnd, baseEnd *string
			if hasH {
				s := h.mode + ":" + h.oid
				headEnd = &s
			}
			if hasB {
				s := b.mode + ":" + b.oid
				baseEnd = &s
			}
			if !strPtrEq(headEnd, baseEnd) {
				conflict = true
				break
			}
		}
	}
	return &pullDiffResult{
		baseTip: baseTip, hasBaseTip: true,
		headTip: headTipOID, hasHeadTip: true,
		baseFlat: baseFlat, headFlat: headFlat, mergeBaseFlat: mergeBaseFlat,
		files: files, conflict: conflict,
	}
}

func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func pullObject(r *repo, pull *pullRec) *JObj {
	baseTip, _ := resolveRef(r, pull.baseRef)
	headTipOID, _ := resolveRef(r, pull.headRef)
	if baseTip == "" {
		baseTip = zeroOID
	}
	if headTipOID == "" {
		headTipOID = zeroOID
	}
	var diff *pullDiffResult
	if pull.state == "open" {
		diff = pullDiff(r, pull)
	}
	mergeable := false
	if diff != nil {
		mergeable = !diff.conflict
	}
	var mergeSHA JVal
	if pull.mergeCommitSha != nil {
		mergeSHA = *pull.mergeCommitSha
	}
	var mergedAt JVal
	if pull.mergedAt != nil {
		mergedAt = *pull.mergedAt
	}
	var closedAt JVal
	if pull.state == "closed" {
		closedAt = fixedISO
	}
	return NewObj(
		KV("id", int64(pull.id)),
		KV("node_id", globalID("PullRequest", r.dbID, pull.number)),
		KV("number", int64(pull.number)),
		KV("title", pull.title),
		KV("body", pull.body),
		KV("state", pull.state),
		KV("user", userJSON()),
		KV("head", NewObj(
			KV("ref", pull.headRef),
			KV("sha", headTipOID),
			KV("label", r.owner+":"+pull.headRef),
			KV("user", userJSON()),
			KV("repo", repoJSON(r)),
		)),
		KV("base", NewObj(
			KV("ref", pull.baseRef),
			KV("sha", baseTip),
			KV("label", r.owner+":"+pull.baseRef),
			KV("user", userJSON()),
			KV("repo", repoJSON(r)),
		)),
		KV("merged", pull.merged),
		KV("merge_commit_sha", mergeSHA),
		KV("merged_at", mergedAt),
		KV("mergeable", mergeable),
		KV("created_at", fixedISO),
		KV("updated_at", fixedISO),
		KV("closed_at", closedAt),
		KV("url", "/api/v3/repos/"+r.owner+"/"+r.name+"/pulls/"+strconv.Itoa(pull.number)),
		KV("html_url", "https://example.invalid/"+r.owner+"/"+r.name+"/pull/"+strconv.Itoa(pull.number)),
	)
}

func pullFilesJSON(r *repo, pull *pullRec) []JVal {
	diff := pullDiff(r, pull)
	if diff == nil {
		return []JVal{}
	}
	var out []JVal
	for _, f := range diff.files {
		additions := 0
		deletions := 0
		if f.status != "removed" {
			additions = lineCountOf(r, f.newOID)
		}
		if f.status != "added" {
			deletions = lineCountOf(r, f.oldOID)
		}
		out = append(out, NewObj(
			KV("filename", f.path),
			KV("status", f.status),
			KV("additions", int64(additions)),
			KV("deletions", int64(deletions)),
			KV("changes", int64(additions+deletions)),
		))
	}
	if out == nil {
		out = []JVal{}
	}
	return out
}

// --- Pagination ---

func parsePageParams(q *query) (page, perPage int) {
	page, perPage = 1, 30
	if p, ok := q.get("page"); ok {
		if n, ok := jsParseInt(p); ok && n >= 1 {
			page = int(n)
		}
	}
	if pp, ok := q.get("per_page"); ok {
		if n, ok := jsParseInt(pp); ok && n >= 1 {
			perPage = int(n)
			if perPage > 100 {
				perPage = 100
			}
		}
	}
	return page, perPage
}

func pageLinkHeader(req *request, path string, q *query, page, perPage, total int) string {
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if totalPages <= 1 {
		return ""
	}
	host := req.headers["host"]
	if host == "" {
		host = "127.0.0.1"
	}
	build := func(p int) string {
		nq := q.clone()
		nq.set("page", strconv.Itoa(p))
		nq.set("per_page", strconv.Itoa(perPage))
		return "http://" + host + path + "?" + nq.String()
	}
	var links []string
	if page < totalPages {
		links = append(links, "<"+build(page+1)+">; rel=\"next\"")
	}
	links = append(links, "<"+build(totalPages)+">; rel=\"last\"")
	if page > 1 {
		links = append(links, "<"+build(1)+">; rel=\"first\"", "<"+build(page-1)+">; rel=\"prev\"")
	}
	return strings.Join(links, ", ")
}

// --- Small REST helpers ---

func stripBranchPrefix(ref string) string {
	if idx := strings.IndexByte(ref, ':'); idx >= 0 {
		return ref[idx+1:]
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		return ref[len("refs/heads/"):]
	}
	return ref
}

func parseLabelInput(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return []string{}
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		} else if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok {
				out = append(out, name)
			}
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func commentJSON(r *repo, issue *issueRec, comment *commentRec) *JObj {
	return NewObj(
		KV("id", int64(comment.id)),
		KV("node_id", globalID("IssueComment", r.dbID, comment.id)),
		KV("body", comment.body),
		KV("user", userJSON()),
		KV("created_at", fixedISO),
		KV("updated_at", fixedISO),
		KV("url", "/api/v3/repos/"+r.owner+"/"+r.name+"/issues/comments/"+strconv.Itoa(comment.id)),
		KV("html_url", "https://example.invalid/"+r.owner+"/"+r.name+"/issues/"+strconv.Itoa(issue.number)+"#issuecomment-"+strconv.Itoa(comment.id)),
	)
}

// dirListing returns the immediate children of the tree at prefix, or
// nil when the directory does not exist.
func dirListing(r *repo, commitOID string, flat map[string]flatEntry, prefix string) []JVal {
	view := commitViewOf(r, commitOID)
	if view == nil {
		return nil
	}
	treeOID := view.tree
	var segments []string
	if prefix != "" {
		segments = strings.Split(prefix[:len(prefix)-1], "/")
	}
	for _, seg := range segments {
		entries := treeEntriesOf(r, treeOID)
		if entries == nil {
			return nil
		}
		found := false
		for _, e := range entries {
			if e.name == seg && e.mode == "40000" {
				treeOID = e.oid
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	entries := treeEntriesOf(r, treeOID)
	if entries == nil {
		return nil
	}
	var out []JVal
	for _, e := range entries {
		childPath := prefix + e.name
		if e.mode == "40000" {
			out = append(out, NewObj(
				KV("type", "dir"),
				KV("name", e.name),
				KV("path", childPath),
				KV("sha", e.oid),
			))
		} else {
			size := 0
			if obj, ok := r.objects[e.oid]; ok {
				size = len(obj.data)
			}
			out = append(out, NewObj(
				KV("type", "file"),
				KV("name", e.name),
				KV("path", childPath),
				KV("sha", e.oid),
				KV("size", int64(size)),
			))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ni, _ := out[i].(*JObj).Get("name")
		nj, _ := out[j].(*JObj).Get("name")
		si, _ := ni.(string)
		sj, _ := nj.(string)
		return si < sj
	})
	if out == nil {
		out = []JVal{}
	}
	return out
}

func rootListing(r *repo, commitOID string, flat map[string]flatEntry) []JVal {
	if l := dirListing(r, commitOID, flat, ""); l != nil {
		return l
	}
	return []JVal{}
}
