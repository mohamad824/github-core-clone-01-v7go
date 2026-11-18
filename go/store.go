// Repository model: refs, immutable object store, issues/pulls/labels,
// fetch closure computation, advertisements, and receive-pack.

package main

import (
	"fmt"
	"sort"
	"strings"
)

type flatEntry struct {
	mode string
	oid  string
}

type labelRec struct {
	name        string
	color       string
	description string
}

type commentRec struct {
	id   int
	body string
}

type issueRec struct {
	id         int
	number     int
	title      string
	body       string
	state      string // "open" | "closed"
	labels     []string
	createdSeq int
	comments   []*commentRec
	closedAt   *string
}

type pullRec struct {
	id             int
	number         int
	title          string
	body           string
	headRef        string
	baseRef        string
	state          string // "open" | "closed"
	merged         bool
	mergeCommitSha *string
	mergedAt       *string
	createdSeq     int
}

type repo struct {
	dbID          int
	name          string
	owner         string
	description   string
	isPrivate     bool
	defaultBranch string
	refs          map[string]string
	objects       map[string]*gitObject
	commitCache   map[string]*commitView
	treeCache     map[string][]treeEntry
	flatCache     map[string]map[string]flatEntry
	labels        map[string]*labelRec
	labelOrder    []string
	issues        map[int]*issueRec
	pulls         map[int]*pullRec
	seq           int
	commitCounter int64
	version       int
	adV0Upload    []byte
	adV0Receive   []byte
	adV2          []byte
	lsRefsCache   map[string][]byte
	fetchCache    map[string][]byte
	issueBufCache map[int][]byte
}

var (
	nextRepoDbID  = 1
	nextObjectID  = 1
	nextCommentID = 1
	reposByKey    = map[string]*repo{}
	reposByID     = map[int]*repo{}
)

func repoKey(owner, name string) string { return owner + "/" + name }

func newRepo(name, owner, description string, isPrivate bool, defaultBranch string) *repo {
	dbID := nextRepoDbID
	nextRepoDbID++
	return &repo{
		dbID:          dbID,
		name:          name,
		owner:         owner,
		description:   description,
		isPrivate:     isPrivate,
		defaultBranch: defaultBranch,
		refs:          map[string]string{},
		objects:       map[string]*gitObject{},
		commitCache:   map[string]*commitView{},
		treeCache:     map[string][]treeEntry{},
		flatCache:     map[string]map[string]flatEntry{},
		labels:        map[string]*labelRec{},
		issues:        map[int]*issueRec{},
		pulls:         map[int]*pullRec{},
		lsRefsCache:   map[string][]byte{},
		fetchCache:    map[string][]byte{},
		issueBufCache: map[int][]byte{},
	}
}

func bumpVersion(r *repo) {
	r.version++
	r.adV0Upload = nil
	r.adV0Receive = nil
	r.adV2 = nil
	r.lsRefsCache = map[string][]byte{}
	r.fetchCache = map[string][]byte{}
}

func storeObject(r *repo, obj *gitObject) string {
	oid := hashObject(obj.typ, obj.data)
	if _, ok := r.objects[oid]; !ok {
		r.objects[oid] = obj
	}
	return oid
}

func commitViewOf(r *repo, oid string) *commitView {
	if v, ok := r.commitCache[oid]; ok {
		return v
	}
	obj, ok := r.objects[oid]
	if !ok || obj.typ != objCommit {
		return nil
	}
	v := parseCommit(obj.data)
	r.commitCache[oid] = v
	return v
}

func treeEntriesOf(r *repo, oid string) []treeEntry {
	if e, ok := r.treeCache[oid]; ok {
		return e
	}
	obj, ok := r.objects[oid]
	if !ok || obj.typ != objTree {
		return nil
	}
	entries := parseTree(obj.data)
	r.treeCache[oid] = entries
	return entries
}

func peelToCommit(r *repo, oid string) (string, bool) {
	cur := oid
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		if seen[cur] {
			return "", false
		}
		seen[cur] = true
		obj, ok := r.objects[cur]
		if !ok {
			return "", false
		}
		if obj.typ == objCommit {
			return cur, true
		}
		if obj.typ == objTag {
			cur = parseTag(obj.data).object
			continue
		}
		return "", false
	}
	return "", false
}

func peelTagChain(r *repo, oid string) (string, bool) {
	cur := oid
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		if seen[cur] {
			return "", false
		}
		seen[cur] = true
		obj, ok := r.objects[cur]
		if !ok {
			return "", false
		}
		if obj.typ != objTag {
			return cur, true
		}
		cur = parseTag(obj.data).object
	}
	return "", false
}

func headRefName(r *repo) string { return "refs/heads/" + r.defaultBranch }

func headTip(r *repo) (string, bool) {
	tip, ok := r.refs[headRefName(r)]
	return tip, ok
}

func flatMapOf(r *repo, commitOID string) map[string]flatEntry {
	if m, ok := r.flatCache[commitOID]; ok {
		return m
	}
	view := commitViewOf(r, commitOID)
	if view == nil {
		return nil
	}
	out := map[string]flatEntry{}
	type frame struct {
		treeOID string
		prefix  string
	}
	stack := []frame{{view.tree, ""}}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries := treeEntriesOf(r, cur.treeOID)
		if entries == nil {
			continue
		}
		for _, e := range entries {
			path := cur.prefix + e.name
			if e.mode == "40000" {
				stack = append(stack, frame{e.oid, path + "/"})
			} else {
				out[path] = flatEntry{mode: e.mode, oid: e.oid}
			}
		}
	}
	r.flatCache[commitOID] = out
	return out
}

func buildTreeFromFlat(r *repo, flat map[string]flatEntry) string {
	type node struct {
		files map[string]flatEntry
		dirs  map[string]*node
	}
	root := &node{files: map[string]flatEntry{}, dirs: map[string]*node{}}
	for path, entry := range flat {
		parts := strings.Split(path, "/")
		n := root
		for i := 0; i < len(parts)-1; i++ {
			child, ok := n.dirs[parts[i]]
			if !ok {
				child = &node{files: map[string]flatEntry{}, dirs: map[string]*node{}}
				n.dirs[parts[i]] = child
			}
			n = child
		}
		n.files[parts[len(parts)-1]] = entry
	}
	var build func(n *node) string
	build = func(n *node) string {
		var entries []treeEntry
		for name, entry := range n.files {
			entries = append(entries, treeEntry{mode: entry.mode, name: name, oid: entry.oid})
		}
		for name, child := range n.dirs {
			entries = append(entries, treeEntry{mode: "40000", name: name, oid: build(child)})
		}
		return storeObject(r, &gitObject{typ: objTree, data: buildTreeBytes(entries)})
	}
	return build(root)
}

func isAncestor(r *repo, ancOID, descOID string) bool {
	anc, okA := peelToCommit(r, ancOID)
	desc, okD := peelToCommit(r, descOID)
	if !okA || !okD {
		return ancOID == descOID
	}
	if anc == desc {
		return true
	}
	seen := map[string]bool{}
	stack := []string{desc}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == anc {
			return true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		view := commitViewOf(r, cur)
		if view == nil {
			continue
		}
		for _, p := range view.parents {
			if !seen[p] {
				stack = append(stack, p)
			}
		}
	}
	return false
}

func mergeBase(r *repo, aOID, bOID string) (string, bool) {
	a, okA := peelToCommit(r, aOID)
	b, okB := peelToCommit(r, bOID)
	if !okA || !okB {
		return "", false
	}
	if a == b {
		return a, true
	}
	ancestorsA := map[string]bool{}
	stackA := []string{a}
	for len(stackA) > 0 {
		cur := stackA[len(stackA)-1]
		stackA = stackA[:len(stackA)-1]
		if ancestorsA[cur] {
			continue
		}
		ancestorsA[cur] = true
		if view := commitViewOf(r, cur); view != nil {
			stackA = append(stackA, view.parents...)
		}
	}
	seen := map[string]bool{}
	queue := []string{b}
	for qi := 0; qi < len(queue); qi++ {
		cur := queue[qi]
		if ancestorsA[cur] {
			return cur, true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		if view := commitViewOf(r, cur); view != nil {
			queue = append(queue, view.parents...)
		}
	}
	return "", false
}

func resolveRef(r *repo, ref string) (string, bool) {
	if ref == "HEAD" {
		return headTip(r)
	}
	if oid, ok := r.refs[ref]; ok {
		return oid, true
	}
	if oid, ok := r.refs["refs/heads/"+ref]; ok {
		return oid, true
	}
	if oid, ok := r.refs["refs/tags/"+ref]; ok {
		return oid, true
	}
	if isOID(ref) {
		if _, ok := r.objects[ref]; ok {
			return ref, true
		}
	}
	return "", false
}

type refPair struct {
	name string
	oid  string
}

func sortedRefList(r *repo) []refPair {
	list := make([]refPair, 0, len(r.refs))
	for name, oid := range r.refs {
		list = append(list, refPair{name, oid})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list
}

// commitOnBranch creates a server-authored commit on branch. mutate edits
// a copy of the parent's flat map.
func commitOnBranch(r *repo, branch, message string, mutate func(flat map[string]flatEntry)) string {
	refName := "refs/heads/" + branch
	parent, ok := r.refs[refName]
	if !ok && branch != r.defaultBranch {
		parent, ok = headTip(r)
	}
	flat := map[string]flatEntry{}
	if ok {
		if parentFlat := flatMapOf(r, parent); parentFlat != nil {
			for p, e := range parentFlat {
				flat[p] = e
			}
		}
	}
	mutate(flat)
	treeOID := buildTreeFromFlat(r, flat)
	epoch := fixedEpochSec + r.commitCounter
	r.commitCounter++
	ident := identLine("forge", "forge@make.fun", epoch)
	var parents []string
	if ok {
		parents = []string{parent}
	}
	commitOID := storeObject(r, &gitObject{typ: objCommit, data: buildCommitBytes(treeOID, parents, ident, ident, message, "")})
	r.refs[refName] = commitOID
	bumpVersion(r)
	return commitOID
}

// --- Fetch closure ---

type fetchRequest struct {
	wants   []string
	haves   []string
	shallow []string
	depth   *int
}

type fetchClosureResult struct {
	send         []*gitObject
	shallowOut   []string
	unshallowOut []string
}

func includeTreeClosure(r *repo, treeOID string, out map[string]*gitObject) {
	stack := []string{treeOID}
	seen := map[string]bool{}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		obj, ok := r.objects[cur]
		if !ok || obj.typ != objTree {
			continue
		}
		out[cur] = obj
		entries := treeEntriesOf(r, cur)
		if entries == nil {
			continue
		}
		for _, e := range entries {
			if e.mode == "40000" {
				stack = append(stack, e.oid)
			} else if e.mode == "160000" {
				continue
			} else {
				if child, ok := r.objects[e.oid]; ok {
					out[e.oid] = child
				}
			}
		}
	}
}

func fetchClosure(r *repo, req *fetchRequest) *fetchClosureResult {
	included := map[string]*gitObject{}
	commitDepth := map[string]int{}
	type queueItem struct {
		oid   string
		depth int
	}
	queue := make([]queueItem, 0, len(req.wants))
	for _, w := range req.wants {
		queue = append(queue, queueItem{w, 0})
	}
	for qi := 0; qi < len(queue); qi++ {
		item := queue[qi]
		obj, ok := r.objects[item.oid]
		if !ok {
			continue
		}
		if obj.typ == objTag {
			included[item.oid] = obj
			target := parseTag(obj.data).object
			queue = append(queue, queueItem{target, item.depth})
			continue
		}
		if obj.typ != objCommit {
			included[item.oid] = obj
			continue
		}
		if prev, ok := commitDepth[item.oid]; ok && prev <= item.depth {
			continue
		}
		commitDepth[item.oid] = item.depth
		included[item.oid] = obj
		view := commitViewOf(r, item.oid)
		if view == nil {
			continue
		}
		includeTreeClosure(r, view.tree, included)
		if req.depth == nil || item.depth+1 < *req.depth {
			for _, p := range view.parents {
				queue = append(queue, queueItem{p, item.depth + 1})
			}
		}
	}
	var shallowOut []string
	if req.depth != nil {
		for oid := range commitDepth {
			view := commitViewOf(r, oid)
			if view == nil {
				continue
			}
			for _, p := range view.parents {
				if _, stored := r.objects[p]; stored {
					if _, in := commitDepth[p]; !in {
						shallowOut = append(shallowOut, oid)
						break
					}
				}
			}
		}
		sort.Strings(shallowOut)
	}
	var unshallowOut []string
	for _, s := range req.shallow {
		if _, ok := commitDepth[s]; !ok {
			continue
		}
		view := commitViewOf(r, s)
		if view == nil {
			continue
		}
		allIn := true
		for _, p := range view.parents {
			if _, stored := r.objects[p]; stored {
				if _, in := commitDepth[p]; !in {
					allIn = false
					break
				}
			}
		}
		if allIn {
			unshallowOut = append(unshallowOut, s)
		}
	}
	sort.Strings(unshallowOut)
	excluded := map[string]*gitObject{}
	shallowSet := map[string]bool{}
	for _, s := range req.shallow {
		shallowSet[s] = true
	}
	exQueue := append([]string{}, req.haves...)
	exSeen := map[string]bool{}
	for ei := 0; ei < len(exQueue); ei++ {
		oid := exQueue[ei]
		if exSeen[oid] {
			continue
		}
		exSeen[oid] = true
		obj, ok := r.objects[oid]
		if !ok {
			continue
		}
		excluded[oid] = obj
		if obj.typ == objTag {
			exQueue = append(exQueue, parseTag(obj.data).object)
			continue
		}
		if obj.typ != objCommit {
			continue
		}
		view := commitViewOf(r, oid)
		if view == nil {
			continue
		}
		includeTreeClosure(r, view.tree, excluded)
		if shallowSet[oid] {
			continue
		}
		exQueue = append(exQueue, view.parents...)
	}
	type oidObj struct {
		oid string
		obj *gitObject
	}
	var send []oidObj
	for oid, obj := range included {
		if _, ex := excluded[oid]; !ex {
			send = append(send, oidObj{oid, obj})
		}
	}
	sort.Slice(send, func(i, j int) bool { return send[i].oid < send[j].oid })
	objs := make([]*gitObject, len(send))
	for i, s := range send {
		objs[i] = s.obj
	}
	return &fetchClosureResult{send: objs, shallowOut: shallowOut, unshallowOut: unshallowOut}
}

// --- Advertisements and ls-refs ---

func v0RefLines(r *repo, caps string) []string {
	refs := sortedRefList(r)
	var lines []string
	capSuffix := "\x00" + caps
	if len(refs) == 0 {
		return []string{zeroOID + " capabilities^{}" + capSuffix + "\n"}
	}
	tip, hasTip := headTip(r)
	if hasTip {
		lines = append(lines, tip+" HEAD"+capSuffix+"\n")
	}
	first := !hasTip
	for _, rp := range refs {
		suffix := ""
		if first {
			suffix = capSuffix
		}
		first = false
		lines = append(lines, rp.oid+" "+rp.name+suffix+"\n")
		if obj, ok := r.objects[rp.oid]; ok && obj.typ == objTag {
			if peeled, ok := peelTagChain(r, rp.oid); ok && peeled != rp.oid {
				lines = append(lines, peeled+" "+rp.name+"^{}\n")
			}
		}
	}
	return lines
}

func buildV0Advertisement(r *repo, service string) []byte {
	if service == "git-upload-pack" && r.adV0Upload != nil {
		return r.adV0Upload
	}
	if service == "git-receive-pack" && r.adV0Receive != nil {
		return r.adV0Receive
	}
	var caps string
	if service == "git-upload-pack" {
		caps = "side-band-64k shallow thin-pack ofs-delta agent=" + forgeAgent + " symref=HEAD:" + headRefName(r)
	} else {
		caps = "report-status side-band-64k delete-refs ofs-delta agent=" + forgeAgent
	}
	var out []byte
	out = append(out, pktString("# service="+service+"\n")...)
	out = append(out, pktFlush...)
	out = append(out, pktTextLines(v0RefLines(r, caps))...)
	out = append(out, pktFlush...)
	if service == "git-upload-pack" {
		r.adV0Upload = out
	} else {
		r.adV0Receive = out
	}
	return out
}

func buildV2Advertisement(r *repo) []byte {
	if r.adV2 != nil {
		return r.adV2
	}
	var out []byte
	out = append(out, pktString("version 2\n")...)
	out = append(out, pktString("ls-refs=unborn\n")...)
	out = append(out, pktString("fetch=shallow\n")...)
	out = append(out, pktString("object-format=sha1\n")...)
	out = append(out, pktString("agent="+forgeAgent+"\n")...)
	out = append(out, pktFlush...)
	r.adV2 = out
	return out
}

type lsRefsOpts struct {
	peel     bool
	symrefs  bool
	unborn   bool
	prefixes []string
}

func buildLsRefs(r *repo, opts *lsRefsOpts) []byte {
	key := fmt.Sprintf("%d|%d|%d|%d|%s", r.version, b2i(opts.peel), b2i(opts.symrefs), b2i(opts.unborn), strings.Join(opts.prefixes, "\n"))
	if cached, ok := r.lsRefsCache[key]; ok {
		return cached
	}
	matches := func(name string) bool {
		if len(opts.prefixes) == 0 {
			return true
		}
		for _, p := range opts.prefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
		return false
	}
	var lines []string
	refs := sortedRefList(r)
	if len(refs) == 0 {
		if opts.unborn {
			lines = append(lines, "unborn HEAD symref-target:"+headRefName(r)+"\n")
		}
	} else {
		if tip, ok := headTip(r); ok && matches("HEAD") {
			line := tip + " HEAD"
			if opts.symrefs {
				line += " symref-target:" + headRefName(r)
			}
			lines = append(lines, line+"\n")
		}
		for _, rp := range refs {
			if !matches(rp.name) {
				continue
			}
			line := rp.oid + " " + rp.name
			if opts.peel {
				if obj, ok := r.objects[rp.oid]; ok && obj.typ == objTag {
					if peeled, ok := peelTagChain(r, rp.oid); ok && peeled != rp.oid {
						line += " peeled:" + peeled
					}
				}
			}
			lines = append(lines, line+"\n")
		}
	}
	var out []byte
	out = append(out, pktTextLines(lines)...)
	out = append(out, pktFlush...)
	r.lsRefsCache[key] = out
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Fetch response builders ---

func fetchCacheKey(protocol string, version int, req *fetchRequest) string {
	depth := ""
	if req.depth != nil {
		depth = fmt.Sprint(*req.depth)
	}
	return protocol + "|" + fmt.Sprint(version) + "|w=" + strings.Join(req.wants, ",") +
		"|h=" + strings.Join(req.haves, ",") + "|s=" + strings.Join(req.shallow, ",") + "|d=" + depth
}

func buildFetchV0(r *repo, req *fetchRequest) []byte {
	key := fetchCacheKey("v0", r.version, req)
	if cached, ok := r.fetchCache[key]; ok {
		return cached
	}
	closure := fetchClosure(r, req)
	var lines []string
	for _, s := range closure.shallowOut {
		lines = append(lines, "shallow "+s+"\n")
	}
	for _, u := range closure.unshallowOut {
		lines = append(lines, "unshallow "+u+"\n")
	}
	knownHave := ""
	for _, h := range req.haves {
		if _, ok := r.objects[h]; ok {
			knownHave = h
			break
		}
	}
	if knownHave != "" {
		lines = append(lines, "ACK "+knownHave+"\n")
	} else {
		lines = append(lines, "NAK\n")
	}
	pack := writePack(closure.send)
	var out []byte
	out = append(out, pktTextLines(lines)...)
	out = append(out, sidebandWrap(pack)...)
	out = append(out, pktFlush...)
	if len(r.fetchCache) > 64 {
		r.fetchCache = map[string][]byte{}
	}
	r.fetchCache[key] = out
	return out
}

func buildFetchV2(r *repo, req *fetchRequest) []byte {
	key := fetchCacheKey("v2", r.version, req)
	if cached, ok := r.fetchCache[key]; ok {
		return cached
	}
	closure := fetchClosure(r, req)
	var parts []byte
	parts = append(parts, pktString("acknowledgments\n")...)
	knownHave := ""
	for _, h := range req.haves {
		if _, ok := r.objects[h]; ok {
			knownHave = h
			break
		}
	}
	if knownHave != "" {
		parts = append(parts, pktString("ACK "+knownHave+"\n")...)
		parts = append(parts, pktString("ready\n")...)
	} else {
		parts = append(parts, pktString("NAK\n")...)
	}
	parts = append(parts, pktDelim...)
	if len(closure.shallowOut) > 0 || len(closure.unshallowOut) > 0 {
		parts = append(parts, pktString("shallow-info\n")...)
		for _, s := range closure.shallowOut {
			parts = append(parts, pktString("shallow "+s+"\n")...)
		}
		for _, u := range closure.unshallowOut {
			parts = append(parts, pktString("unshallow "+u+"\n")...)
		}
		parts = append(parts, pktDelim...)
	}
	parts = append(parts, pktString("packfile\n")...)
	parts = append(parts, sidebandWrap(writePack(closure.send))...)
	parts = append(parts, pktFlush...)
	if len(r.fetchCache) > 64 {
		r.fetchCache = map[string][]byte{}
	}
	r.fetchCache[key] = parts
	return parts
}

// --- Receive-pack ---

type refCommand struct {
	oldOID string
	newOID string
	ref    string
}

func parseRefCommands(payloads [][]byte) ([]refCommand, string, error) {
	var commands []refCommand
	caps := ""
	for i, p := range payloads {
		text := string(p)
		if strings.HasSuffix(text, "\n") {
			text = text[:len(text)-1]
		}
		if i == 0 {
			if nul := strings.IndexByte(text, 0); nul >= 0 {
				caps = text[nul+1:]
				text = text[:nul]
			}
		}
		m := parseRefCommandLine(text)
		if m == nil {
			return nil, "", &pktError{"bad ref command " + JSONString(text)}
		}
		commands = append(commands, *m)
	}
	return commands, caps, nil
}

// parseRefCommandLine matches /^([0-9a-f]{40}) ([0-9a-f]{40}) (\S+)$/.
func parseRefCommandLine(text string) *refCommand {
	if len(text) < 40+1+40+1+1 {
		return nil
	}
	if !isOID(text[0:40]) || text[40] != ' ' || !isOID(text[41:81]) || text[81] != ' ' {
		return nil
	}
	ref := text[82:]
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
			return nil
		}
	}
	return &refCommand{oldOID: text[0:40], newOID: text[41:81], ref: ref}
}

func receivePack(r *repo, body []byte) ([]byte, error) {
	payloads, rest, err := splitPktAndRest(body)
	if err != nil {
		return nil, err
	}
	commands, caps, err := parseRefCommands(payloads)
	if err != nil {
		return nil, err
	}
	if len(commands) == 0 {
		return nil, &pktError{"empty command list"}
	}
	wantSideband := false
	for _, tok := range strings.Split(caps, " ") {
		if tok == "side-band-64k" || tok == "side-band" {
			wantSideband = true
			break
		}
	}
	unpackDetail := "ok"
	if len(rest) > 0 {
		found, err := readPack(rest, func(oid string) *gitObject { return r.objects[oid] })
		if err != nil {
			unpackDetail = err.Error()
		} else {
			for oid, obj := range found {
				if _, ok := r.objects[oid]; !ok {
					r.objects[oid] = obj
				}
			}
		}
	}
	report := []string{"unpack " + unpackDetail + "\n"}
	refChanged := false
	if unpackDetail == "ok" {
		for _, cmd := range commands {
			current, ok := r.refs[cmd.ref]
			if !ok {
				current = zeroOID
			}
			resultOK := false
			reason := ""
			if cmd.newOID == zeroOID {
				if current == zeroOID {
					reason = "remote ref does not exist"
				} else if cmd.oldOID != current {
					reason = "non-fast-forward"
				} else {
					delete(r.refs, cmd.ref)
					refChanged = true
					resultOK = true
				}
			} else if cmd.oldOID == zeroOID {
				obj, hasObj := r.objects[cmd.newOID]
				if current != zeroOID {
					reason = "non-fast-forward"
				} else if !hasObj {
					reason = "missing object"
				} else if obj.typ != objCommit && obj.typ != objTag {
					reason = "invalid object type"
				} else {
					r.refs[cmd.ref] = cmd.newOID
					refChanged = true
					resultOK = true
				}
			} else {
				obj, hasObj := r.objects[cmd.newOID]
				if current != cmd.oldOID {
					reason = "non-fast-forward"
				} else if !hasObj {
					reason = "missing object"
				} else if cmd.newOID == cmd.oldOID {
					resultOK = true
				} else if obj.typ != objCommit && obj.typ != objTag {
					reason = "invalid object type"
				} else if !isAncestor(r, cmd.oldOID, cmd.newOID) {
					reason = "non-fast-forward"
				} else {
					r.refs[cmd.ref] = cmd.newOID
					refChanged = true
					resultOK = true
				}
			}
			if resultOK {
				report = append(report, "ok "+cmd.ref+"\n")
			} else {
				report = append(report, "ng "+cmd.ref+" "+reason+"\n")
			}
		}
	} else {
		for _, cmd := range commands {
			report = append(report, "ng "+cmd.ref+" unpack failed\n")
		}
	}
	if refChanged {
		bumpVersion(r)
	}
	var inner []byte
	inner = append(inner, pktTextLines(report)...)
	inner = append(inner, pktFlush...)
	if wantSideband {
		var out []byte
		out = append(out, sidebandWrap(inner)...)
		out = append(out, pktFlush...)
		return out, nil
	}
	return inner, nil
}
