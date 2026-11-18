// Git smart HTTP handlers: info/refs advertisement, upload-pack (v0
// and v2), and receive-pack (v0).

package main

import (
	"strings"
)

func parseV0Fetch(body []byte) (*fetchRequest, error) {
	var wants, haves, shallow []string
	var depth *int
	off := 0
	firstLine := true
	for off < len(body) {
		items, next, err := parsePktStream(body, off)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.kind != pktData {
				continue
			}
			text := string(it.payload)
			if strings.HasSuffix(text, "\n") {
				text = text[:len(text)-1]
			}
			if firstLine {
				if nul := strings.IndexByte(text, 0); nul >= 0 {
					text = text[:nul]
				}
			}
			firstLine = false
			switch {
			case strings.HasPrefix(text, "want "):
				wants = append(wants, strings.TrimSpace(text[5:]))
			case strings.HasPrefix(text, "have "):
				haves = append(haves, strings.TrimSpace(text[5:]))
			case strings.HasPrefix(text, "shallow "):
				shallow = append(shallow, strings.TrimSpace(text[8:]))
			case strings.HasPrefix(text, "deepen "):
				if n, ok := jsParseInt(text[7:]); ok && n >= 0 {
					d := int(n)
					depth = &d
				}
			}
		}
		off = next
		if off >= len(body) {
			break
		}
	}
	if len(wants) == 0 {
		return nil, &pktError{"no want lines in upload-pack request"}
	}
	return &fetchRequest{wants: wants, haves: haves, shallow: shallow, depth: depth}, nil
}

type v2Request struct {
	command string
	args    []string
}

func parseV2Request(body []byte) (*v2Request, error) {
	var segments [][]string
	off := 0
	var current []string
	sawFlush := false
	for off < len(body) {
		items, next, err := parsePktStream(body, off)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.kind == pktData {
				text := string(it.payload)
				if strings.HasSuffix(text, "\n") {
					text = text[:len(text)-1]
				}
				current = append(current, text)
			} else {
				segments = append(segments, current)
				current = nil
				if it.kind == pktFlushKind {
					sawFlush = true
				}
			}
		}
		off = next
		if sawFlush {
			break
		}
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	var first []string
	if len(segments) > 0 {
		first = segments[0]
	}
	commandLine := ""
	if len(first) > 0 {
		commandLine = first[0]
	}
	if !strings.HasPrefix(commandLine, "command=") {
		return nil, &pktError{"missing command line in v2 request"}
	}
	command := strings.TrimSpace(commandLine[len("command="):])
	var args []string
	if len(segments) > 1 {
		args = segments[1]
	}
	return &v2Request{command: command, args: args}, nil
}

func looksLikeV2(body []byte) bool {
	items, _, err := parsePktStream(body, 0)
	if err != nil {
		return false
	}
	for _, it := range items {
		if it.kind == pktData {
			return strings.HasPrefix(string(it.payload), "command=")
		}
	}
	return false
}

func handleGit(req *request, owner, repoName, tail string, q *query) response {
	method := req.method
	if !isAuthorized(req) {
		return sendError(401, "Bad credentials")
	}
	r, hasRepo := reposByKey[repoKey(owner, repoName)]

	if tail == "info/refs" {
		if method != "GET" {
			return sendError(404, "Not Found")
		}
		service, _ := q.get("service")
		if service != "git-upload-pack" && service != "git-receive-pack" {
			return sendError(400, "Unknown service")
		}
		if !hasRepo {
			return sendError(404, "Not Found")
		}
		isV2 := strings.Contains(req.headers["git-protocol"], "version=2")
		if service == "git-upload-pack" && isV2 {
			return sendBuffer(200, buildV2Advertisement(r), "application/x-git-upload-pack-advertisement")
		}
		contentType := "application/x-git-upload-pack-advertisement"
		if service == "git-receive-pack" {
			contentType = "application/x-git-receive-pack-advertisement"
		}
		return sendBuffer(200, buildV0Advertisement(r, service), contentType)
	}

	if tail == "git-upload-pack" {
		if method != "POST" {
			return sendError(404, "Not Found")
		}
		if !hasRepo {
			return sendError(404, "Not Found")
		}
		return guardedGit(func() response {
			return handleUploadPack(req, r)
		}, func() response { return sendError(400, "Malformed upload-pack request") })
	}

	if tail == "git-receive-pack" {
		if method != "POST" {
			return sendError(404, "Not Found")
		}
		if !hasRepo {
			return sendError(404, "Not Found")
		}
		return guardedGit(func() response {
			resp, err := receivePack(r, req.body)
			if err != nil {
				return sendError(400, "Malformed receive-pack request")
			}
			return sendBuffer(200, resp, "application/x-git-receive-pack-result")
		}, func() response { return sendError(400, "Malformed receive-pack request") })
	}

	return sendError(404, "Not Found")
}

// guardedGit runs fn, converting any panic into the fallback response —
// mirroring the reference implementation's try/catch around pack
// processing, which must never take the server down.
func guardedGit(fn func() response, fallback func() response) (out response) {
	defer func() {
		if recover() != nil {
			out = fallback()
		}
	}()
	return fn()
}

func handleUploadPack(req *request, r *repo) response {
	body := req.body
	isV2 := strings.Contains(req.headers["git-protocol"], "version=2") || looksLikeV2(body)
	if isV2 {
		v2, err := parseV2Request(body)
		if err != nil {
			return sendError(400, "Malformed upload-pack request")
		}
		if v2.command == "ls-refs" {
			opts := &lsRefsOpts{}
			for _, arg := range v2.args {
				switch {
				case arg == "peel":
					opts.peel = true
				case arg == "symrefs":
					opts.symrefs = true
				case arg == "unborn":
					opts.unborn = true
				case strings.HasPrefix(arg, "ref-prefix "):
					opts.prefixes = append(opts.prefixes, arg[len("ref-prefix "):])
				}
			}
			return sendBuffer(200, buildLsRefs(r, opts), "application/x-git-upload-pack-result")
		}
		if v2.command == "fetch" {
			var wants, haves, shallow []string
			var depth *int
			for _, arg := range v2.args {
				switch {
				case strings.HasPrefix(arg, "want "):
					wants = append(wants, strings.TrimSpace(arg[5:]))
				case strings.HasPrefix(arg, "have "):
					haves = append(haves, strings.TrimSpace(arg[5:]))
				case strings.HasPrefix(arg, "shallow "):
					shallow = append(shallow, strings.TrimSpace(arg[8:]))
				case strings.HasPrefix(arg, "deepen "):
					if n, ok := jsParseInt(arg[7:]); ok && n >= 0 {
						d := int(n)
						depth = &d
					}
				}
			}
			if len(wants) == 0 {
				return sendError(400, "fetch requires a want line")
			}
			return sendBuffer(200, buildFetchV2(r, &fetchRequest{wants: wants, haves: haves, shallow: shallow, depth: depth}), "application/x-git-upload-pack-result")
		}
		return sendError(400, "Unknown command: "+v2.command)
	}
	fetchReq, err := parseV0Fetch(body)
	if err != nil {
		return sendError(400, "Malformed upload-pack request")
	}
	return sendBuffer(200, buildFetchV0(r, fetchReq), "application/x-git-upload-pack-result")
}
