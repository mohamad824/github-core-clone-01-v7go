// Unit tests pinning the wire-critical equivalences: the hand-rolled
// issue/label JSON must be byte-identical to the generic object marshal,
// zlib stream accounting must be exact against Node-produced streams,
// and pack write/read must round-trip.

package main

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestScenarioViewer(t *testing.T) {
	initConfig()
	if viewerLogin != "adaiversen" {
		t.Fatalf("viewer = %q, want adaiversen", viewerLogin)
	}
	if fixedISO != "2025-01-01T00:00:00Z" {
		t.Fatalf("fixedISO = %q", fixedISO)
	}
}

func TestIssueJSONIdentity(t *testing.T) {
	initConfig()
	r := newRepo("t", viewerLogin, "", false, "main")
	label := &labelRec{name: "bug", color: "ff0000", description: "d \"q\" \\ slash"}
	r.labels["bug"] = label
	r.labelOrder = append(r.labelOrder, "bug")
	if got, want := labelJSONString(label, r), string(MarshalJSON(labelJSON(label, r))); got != want {
		t.Fatalf("label JSON mismatch:\n got: %s\nwant: %s", got, want)
	}
	cases := []*issueRec{
		{id: 1, number: 1, title: "plain", body: "simple", state: "open", labels: []string{}, createdSeq: 1},
		{id: 2, number: 2, title: "qu\"ote \\ back\nnewline\ttab émoji 🎉", body: "body \"x\" \\ \x01 control", state: "open", labels: []string{"bug", "missing-label"}, createdSeq: 2},
		{id: 3, number: 3, title: "", body: "", state: "closed", labels: []string{"bug"}, createdSeq: 3, closedAt: strPtr(fixedISO)},
		{id: 4, number: 4, title: "del\x7f and \u2028 sep", body: "bell\x07", state: "open", labels: nil, createdSeq: 4},
	}
	for i, issue := range cases {
		got := issueJSONString(r, issue)
		want := string(MarshalJSON(issueObject(r, issue)))
		if got != want {
			t.Fatalf("case %d mismatch:\n got: %s\nwant: %s", i, got, want)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestJSONStringEscaping(t *testing.T) {
	// Expected values are JSON.stringify outputs (verified against Node).
	cases := map[string]string{
		"a\"b":     `"a\"b"`,
		"a\\b":     `"a\\b"`,
		"a\nb\tc":  `"a\nb\tc"`,
		"a\rb\fc":  `"a\rb\fc"`,
		"a\bb":     `"a\bb"`,
		"\x01\x1f": `"\u0001\u001f"`,
		"a/b<c>&":  `"a/b<c>&"`,
		"émoji 🎉":  `"émoji 🎉"`,
		"\x7f":     "\"\x7f\"",
		"a\u2028b": "\"a\u2028b\"",
	}
	for in, want := range cases {
		if got := JSONString(in); got != want {
			t.Fatalf("JSONString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestJSFloat(t *testing.T) {
	cases := map[float64]string{
		0: "0", 1: "1", -1: "-1", 3.5: "3.5", 0.1: "0.1",
		1e21: "1e+21", 1e-6: "0.000001", 1e-7: "1e-7", 123456789: "123456789",
		1.5e21: "1.5e+21", 0.00001: "0.00001",
	}
	for in, want := range cases {
		if got := jsFloat(in); got != want {
			t.Fatalf("jsFloat(%v) = %s, want %s", in, got, want)
		}
	}
}

func TestQuerySemantics(t *testing.T) {
	q := parseQuery("a=1&b=hello+world&c=%26%3D&a=2&empty&bad=%zz")
	if v, _ := q.get("a"); v != "1" {
		t.Fatalf("get a = %q", v)
	}
	if v, _ := q.get("b"); v != "hello world" {
		t.Fatalf("get b = %q", v)
	}
	if v, _ := q.get("c"); v != "&=" {
		t.Fatalf("get c = %q", v)
	}
	if v, ok := q.get("empty"); !ok || v != "" {
		t.Fatalf("get empty = %q %v", v, ok)
	}
	if v, _ := q.get("bad"); v != "%zz" {
		t.Fatalf("get bad = %q (lenient decode)", v)
	}
	q.set("a", "9")
	if q.String() != "a=9&b=hello+world&c=%26%3D&empty=&bad=%25zz" {
		t.Fatalf("set/string = %q", q.String())
	}
	q.set("page", "2")
	if !strings.HasSuffix(q.String(), "page=2") {
		t.Fatalf("append set = %q", q.String())
	}
}

func TestDecodeSegment(t *testing.T) {
	if s, ok := decodeSegment("a%20b%2Fc"); !ok || s != "a b/c" {
		t.Fatalf("decode = %q %v", s, ok)
	}
	if _, ok := decodeSegment("a%zz"); ok {
		t.Fatalf("bad escape accepted")
	}
	if _, ok := decodeSegment("a%ff"); ok {
		t.Fatalf("invalid UTF-8 accepted")
	}
	if s, ok := decodeSegment("a+b"); !ok || s != "a+b" {
		t.Fatalf("plus = %q", s)
	}
}

func TestInflateAtNodeStreams(t *testing.T) {
	// Streams produced by Node's deflateSync (zlib level 6 default).
	streams := map[string]string{
		"hello world":            "789ccb48cdc9c95728cf2fca4901001a0b045d",
		strings.Repeat("x", 300): "789caba81805c40200b20a8ca1",
		"":                       "789c030000000001",
	}
	for want, hexStream := range streams {
		stream, err := hex.DecodeString(hexStream)
		if err != nil {
			t.Fatal(err)
		}
		full := append(append([]byte{}, stream...), 0xde, 0xad, 0xbe, 0xef)
		data, consumed, err := inflateAt(full, 0)
		if err != nil {
			t.Fatalf("inflate %q: %v", want[:min(8, len(want))], err)
		}
		if consumed != len(stream) {
			t.Fatalf("consumed = %d, stream len = %d", consumed, len(stream))
		}
		if string(data) != want {
			t.Fatalf("data mismatch: got %d bytes", len(data))
		}
	}
}

func TestPackRoundTrip(t *testing.T) {
	blob := &gitObject{typ: objBlob, data: []byte("hello pack\n")}
	tree := &gitObject{typ: objTree, data: buildTreeBytes([]treeEntry{{mode: "100644", name: "f.txt", oid: hashObject(objBlob, blob.data)}})}
	commit := &gitObject{typ: objCommit, data: buildCommitBytes(hashObject(objTree, tree.data), nil, "a <a@x> 1 +0000", "a <a@x> 1 +0000", "msg\n", "")}
	pack := writePack([]*gitObject{blob, tree, commit})
	found, err := readPack(pack, func(oid string) *gitObject { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 {
		t.Fatalf("found %d objects, want 3", len(found))
	}
	for _, obj := range []*gitObject{blob, tree, commit} {
		oid := hashObject(obj.typ, obj.data)
		got, ok := found[oid]
		if !ok {
			t.Fatalf("missing %s", oid)
		}
		if got.typ != obj.typ || !bytes.Equal(got.data, obj.data) {
			t.Fatalf("object %s differs", oid)
		}
	}
	// Corrupt trailer must be rejected.
	bad := append([]byte{}, pack...)
	bad[len(bad)-1] ^= 0xff
	if _, err := readPack(bad, func(oid string) *gitObject { return nil }); err == nil {
		t.Fatalf("corrupt trailer accepted")
	}
	// Empty pack round-trips.
	empty := writePack(nil)
	found, err = readPack(empty, func(oid string) *gitObject { return nil })
	if err != nil || len(found) != 0 {
		t.Fatalf("empty pack: found=%d err=%v", len(found), err)
	}
}

func TestDeltaRoundTrip(t *testing.T) {
	// Hand-built delta: copy 5 bytes from offset 0, insert "!", total 6.
	base := []byte("hello")
	// Delta: copy 5 bytes from offset 0 (opcode 0x90 + size byte 0x05),
	// then insert "!". Result: "hello!".
	delta := []byte{5, 6, 0x90, 0x05, 1, '!'}
	out, err := applyDelta(base, delta)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello!" {
		t.Fatalf("delta result = %q", out)
	}
}
