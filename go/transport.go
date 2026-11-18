// Raw TCP transport: a minimal, protocol-correct HTTP/1.1 server over
// net.Conn, mirroring the reference raw transport's observable behavior:
// request-line/header validation, Content-Length and chunked bodies,
// Expect: 100-continue, keep-alive semantics, single-write responses,
// no Date header, HEAD suppression, and 400/413/431 + close on framing
// errors. Requests on one connection are handled strictly in order.

package main

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"strings"
)

var reasonPhrases = map[int]string{
	200: "OK", 201: "Created", 202: "Accepted", 204: "No Content",
	400: "Bad Request", 401: "Unauthorized", 404: "Not Found",
	405: "Method Not Allowed", 409: "Conflict",
	422: "Unprocessable Entity", 500: "Internal Server Error",
}

const (
	maxHeadBytes = 64 * 1024
	maxBodyBytes = 64 * 1024 * 1024
)

var (
	errParse       = errors.New("parse error")
	errHeaderLarge = errors.New("header too large")
	errBodyLarge   = errors.New("body too large")
)

func isTokenChar(c byte) bool {
	if c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

type parsedHead struct {
	method    string
	target    string
	version   string
	headers   map[string]string
	headerLen int
}

// parseHead parses the head at the front of buf. Returns nil, nil when
// more bytes are needed.
func parseHead(buf []byte) (*parsedHead, error) {
	end := bytes.Index(buf, []byte("\r\n\r\n"))
	if end < 0 {
		if len(buf) > maxHeadBytes {
			return nil, errHeaderLarge
		}
		return nil, nil
	}
	text := string(buf[:end])
	lines := strings.Split(text, "\r\n")
	first := lines[0]
	sp1 := strings.IndexByte(first, ' ')
	sp2 := -1
	if sp1 >= 0 {
		if i := strings.IndexByte(first[sp1+1:], ' '); i >= 0 {
			sp2 = sp1 + 1 + i
		}
	}
	if sp1 <= 0 || sp2 <= sp1+1 || sp2 == len(first)-1 {
		return nil, errParse
	}
	method := first[:sp1]
	target := first[sp1+1 : sp2]
	version := first[sp2+1:]
	for i := 0; i < len(method); i++ {
		if !isTokenChar(method[i]) {
			return nil, errParse
		}
	}
	if len(target) == 0 {
		return nil, errParse
	}
	if version != "HTTP/1.1" && version != "HTTP/1.0" {
		return nil, errParse
	}
	headers := map[string]string{}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			return nil, errParse
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, errParse
		}
		name := strings.ToLower(strings.Trim(line[:colon], " \t"))
		if len(name) == 0 {
			return nil, errParse
		}
		for j := 0; j < len(name); j++ {
			if !isTokenChar(name[j]) {
				return nil, errParse
			}
		}
		value := strings.Trim(line[colon+1:], " \t\r\n\v\f")
		if prev, ok := headers[name]; ok {
			headers[name] = prev + ", " + value
		} else {
			headers[name] = value
		}
	}
	return &parsedHead{method: method, target: target, version: version, headers: headers, headerLen: end + 4}, nil
}

type frameKind int

const (
	frameNone frameKind = iota
	frameLength
	frameChunked
)

type frame struct {
	kind   frameKind
	length int
}

func resolveFrame(headers map[string]string) (frame, error) {
	if te, ok := headers["transfer-encoding"]; ok {
		tokens := strings.Split(strings.ToLower(te), ",")
		for _, t := range tokens {
			if strings.TrimSpace(t) == "chunked" {
				return frame{kind: frameChunked}, nil
			}
		}
		return frame{}, errParse
	}
	cl, ok := headers["content-length"]
	if !ok {
		return frame{kind: frameNone}, nil
	}
	parts := strings.Split(cl, ",")
	length := -1
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !isAllDigits(p) {
			return frame{}, errParse
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return frame{}, errParse
		}
		if length == -1 {
			length = int(n)
		} else if int(n) != length {
			return frame{}, errParse
		}
	}
	if length > maxBodyBytes {
		return frame{}, errBodyLarge
	}
	return frame{kind: frameLength, length: length}, nil
}

// decodeChunked decodes the chunked body starting at buf[start].
// needMore reports that more bytes are required. consumed is relative
// to start.
func decodeChunked(buf []byte, start int) (body []byte, consumed int, needMore bool, err error) {
	var chunks []byte
	pos := start
	total := 0
	for {
		eol := bytes.Index(buf[pos:], []byte("\r\n"))
		if eol < 0 {
			return nil, 0, true, nil
		}
		eol += pos
		sizeLine := string(buf[pos:eol])
		if semi := strings.IndexByte(sizeLine, ';'); semi >= 0 {
			sizeLine = sizeLine[:semi]
		}
		sizeText := strings.TrimSpace(sizeLine)
		if !isHexDigits(sizeText) {
			return nil, 0, false, errParse
		}
		size, perr := strconv.ParseUint(sizeText, 16, 64)
		if perr != nil {
			return nil, 0, false, errParse
		}
		pos = eol + 2
		if size == 0 {
			for {
				tEol := bytes.Index(buf[pos:], []byte("\r\n"))
				if tEol < 0 {
					return nil, 0, true, nil
				}
				tEol += pos
				if tEol == pos {
					return chunks, pos + 2 - start, false, nil
				}
				pos = tEol + 2
			}
		}
		if len(buf) < pos+int(size)+2 {
			return nil, 0, true, nil
		}
		if buf[pos+int(size)] != '\r' || buf[pos+int(size)+1] != '\n' {
			return nil, 0, false, errParse
		}
		chunks = append(chunks, buf[pos:pos+int(size)]...)
		total += int(size)
		if total > maxBodyBytes {
			return nil, 0, false, errBodyLarge
		}
		pos += int(size) + 2
	}
}

func isHexDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if _, ok := hexVal(s[i]); !ok {
			return false
		}
	}
	return true
}

func keepAliveAfter(version string, headers map[string]string) bool {
	if conn, ok := headers["connection"]; ok {
		hasClose, hasKeep := false, false
		for _, t := range strings.Split(strings.ToLower(conn), ",") {
			switch strings.TrimSpace(t) {
			case "close":
				hasClose = true
			case "keep-alive":
				hasKeep = true
			}
		}
		if hasClose {
			return false
		}
		if hasKeep {
			return true
		}
	}
	return version == "HTTP/1.1"
}

func failSocket(conn net.Conn, status int, reason string) {
	msg := "HTTP/1.1 " + strconv.Itoa(status) + " " + reason + "\r\nconnection: close\r\ncontent-length: 0\r\n\r\n"
	_, _ = conn.Write([]byte(msg))
}

func failFor(conn net.Conn, err error) {
	switch err {
	case errBodyLarge:
		failSocket(conn, 413, "Content Too Large")
	case errHeaderLarge:
		failSocket(conn, 431, "Request Header Fields Too Large")
	default:
		failSocket(conn, 400, "Bad Request")
	}
}

func writeResponse(conn net.Conn, resp response, isHead bool) error {
	reason := reasonPhrases[resp.status]
	var b []byte
	b = append(b, "HTTP/1.1 "...)
	b = append(b, strconv.Itoa(resp.status)...)
	if reason != "" {
		b = append(b, ' ')
		b = append(b, reason...)
	}
	b = append(b, '\r', '\n')
	for _, h := range resp.headers {
		b = append(b, h.k...)
		b = append(b, ':', ' ')
		b = append(b, h.v...)
		b = append(b, '\r', '\n')
	}
	b = append(b, '\r', '\n')
	if !isHead {
		b = append(b, resp.body...)
	}
	_, err := conn.Write(b)
	return err
}

func serveConn(conn net.Conn) {
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	var buf []byte
	tmp := make([]byte, 32768)
	for {
		// Parse the head, reading until complete or invalid.
		var head *parsedHead
		for head == nil {
			var err error
			head, err = parseHead(buf)
			if err != nil {
				failFor(conn, err)
				return
			}
			if head != nil {
				break
			}
			n, rerr := conn.Read(tmp)
			if rerr != nil {
				return
			}
			buf = append(buf, tmp[:n]...)
		}
		fr, err := resolveFrame(head.headers)
		if err != nil {
			failFor(conn, err)
			return
		}
		if expect, ok := head.headers["expect"]; ok && strings.Contains(strings.ToLower(expect), "100-continue") {
			if _, werr := conn.Write([]byte("HTTP/1.1 100 Continue\r\n\r\n")); werr != nil {
				return
			}
		}
		// Read the body per the framing.
		var body []byte
		consumed := head.headerLen
		switch fr.kind {
		case frameNone:
			body = []byte{}
		case frameLength:
			for len(buf) < head.headerLen+fr.length {
				n, rerr := conn.Read(tmp)
				if rerr != nil {
					return
				}
				buf = append(buf, tmp[:n]...)
			}
			// Copy off the socket buffer: handlers retain pack slices.
			body = make([]byte, fr.length)
			copy(body, buf[head.headerLen:head.headerLen+fr.length])
			consumed = head.headerLen + fr.length
		case frameChunked:
			for {
				decoded, used, needMore, derr := decodeChunked(buf, head.headerLen)
				if derr != nil {
					failFor(conn, derr)
					return
				}
				if !needMore {
					body = decoded
					consumed = head.headerLen + used
					break
				}
				n, rerr := conn.Read(tmp)
				if rerr != nil {
					return
				}
				buf = append(buf, tmp[:n]...)
			}
		}
		buf = append(buf[:0], buf[consumed:]...)
		keep := keepAliveAfter(head.version, head.headers)
		req := &request{method: head.method, target: head.target, headers: head.headers, body: body}
		mu.Lock()
		resp := dispatch(req)
		mu.Unlock()
		if werr := writeResponse(conn, resp, head.method == "HEAD"); werr != nil {
			return
		}
		if !keep {
			return
		}
	}
}
