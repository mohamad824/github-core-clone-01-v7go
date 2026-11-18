// HTTP plumbing, router, and startup. Responses are built as a single
// status + ordered headers + body, then written in one socket write by
// the transport. All request handling runs under one mutex: the reference
// implementation is single-threaded and its handlers run atomically after
// the body is buffered, so serializing dispatch matches its observable
// semantics exactly (and the benchmark is sequential).

package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

type headerPair struct {
	k, v string
}

type request struct {
	method  string
	target  string
	headers map[string]string
	body    []byte
}

type response struct {
	status  int
	headers []headerPair
	body    []byte
}

var (
	mu             sync.Mutex
	requestCounter int64
)

func nextRequestID() string {
	requestCounter++
	return "req-" + strconv.FormatInt(requestCounter, 10)
}

func isAuthorized(req *request) bool {
	header, ok := req.headers["authorization"]
	if !ok || !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(header[len("Bearer "):])
	if len(token) == 0 {
		return false
	}
	if len(expectedToken) > 0 {
		return token == expectedToken
	}
	return true
}

func sendBuffer(status int, body []byte, contentType string, extra ...headerPair) response {
	headers := []headerPair{
		{"content-type", contentType},
		{"content-length", strconv.Itoa(len(body))},
		{"x-github-request-id", nextRequestID()},
	}
	headers = append(headers, extra...)
	return response{status: status, headers: headers, body: body}
}

func sendBufferExtra(status int, body []byte, contentType string, link string) response {
	if link != "" {
		return sendBuffer(status, body, contentType, headerPair{"link", link})
	}
	return sendBuffer(status, body, contentType)
}

func sendJSON(status int, value JVal, extra ...headerPair) response {
	return sendBuffer(status, MarshalJSON(value), "application/json", extra...)
}

func sendJSONExtra(status int, value JVal, link string) response {
	if link != "" {
		return sendJSON(status, value, headerPair{"link", link})
	}
	return sendJSON(status, value)
}

func sendError(status int, message string) response {
	return sendJSON(status, NewObj(KV("message", message)))
}

func sendNoContent() response {
	return response{status: 204, headers: []headerPair{{"x-github-request-id", nextRequestID()}}}
}

// --- Router ---

func dispatch(req *request) (resp response) {
	defer func() {
		if recover() != nil {
			resp = sendError(500, "Internal Server Error")
		}
	}()
	url := req.target
	qIdx := strings.IndexByte(url, '?')
	rawPath := url
	queryStr := ""
	if qIdx >= 0 {
		rawPath = url[:qIdx]
		queryStr = url[qIdx+1:]
	}
	pathname := rawPath
	if len(pathname) > 1 && pathname[len(pathname)-1] == '/' {
		pathname = strings.TrimRight(pathname, "/")
	}
	if len(pathname) == 0 {
		pathname = "/"
	}
	q := parseQuery(queryStr)
	var rawSegments []string
	for _, s := range strings.Split(pathname, "/") {
		if len(s) > 0 {
			rawSegments = append(rawSegments, s)
		}
	}
	var segments []string
	if !strings.Contains(pathname, "%") {
		segments = rawSegments
	} else {
		for _, s := range rawSegments {
			dec, ok := decodeSegment(s)
			if !ok {
				return sendError(400, "Bad Request")
			}
			segments = append(segments, dec)
		}
	}

	if pathname == "/healthz" {
		if req.method != "GET" {
			return sendError(404, "Not Found")
		}
		return sendJSON(200, NewObj(KV("ok", true)))
	}
	if len(segments) >= 2 && segments[0] == "api" && segments[1] == "v3" {
		return handleRest(req, segments[2:], pathname, q)
	}
	if pathname == "/api/graphql" {
		return handleGraphQL(req)
	}
	if pathname == "/mcp" {
		return handleMcp(req)
	}
	if len(segments) >= 3 && strings.HasSuffix(segments[1], ".git") {
		owner := segments[0]
		repoName := segments[1][:len(segments[1])-4]
		tail := strings.Join(segments[2:], "/")
		return handleGit(req, owner, repoName, tail, q)
	}
	return sendError(404, "Not Found")
}

// --- Startup ---

func parseListenPort() int {
	raw := os.Getenv("PORT")
	if raw == "" {
		return 0
	}
	n, ok := jsParseInt(raw)
	if !ok {
		return 0
	}
	return int(n)
}

func main() {
	initConfig()
	_ = userJSONString() // precompute the shared viewer JSON bytes
	port := parseListenPort()
	ln, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "forge: listen:", err)
		os.Exit(1)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	fmt.Fprintln(os.Stderr, "forge listening on port", actualPort)
	// The launcher reads this line on stdout to learn the bound port.
	fmt.Println("FORGE_PORT=" + strconv.Itoa(actualPort))
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Transient accept errors must not kill the server.
			continue
		}
		go serveConn(conn)
	}
}
