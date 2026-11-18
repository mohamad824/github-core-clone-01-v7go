// Git object model, pkt-line codec, and pack read/write. Pack reading
// handles plain objects plus OFS_DELTA and REF_DELTA with delta
// application, size verification, and trailer checksum; pack writing
// emits plain packs (no deltas), which the protocol allows.

package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

type objType int

const (
	objCommit objType = 1
	objTree   objType = 2
	objBlob   objType = 3
	objTag    objType = 4
)

func (t objType) name() string {
	switch t {
	case objCommit:
		return "commit"
	case objTree:
		return "tree"
	case objBlob:
		return "blob"
	case objTag:
		return "tag"
	}
	return ""
}

func typeFromName(s string) (objType, bool) {
	switch s {
	case "commit":
		return objCommit, true
	case "tree":
		return objTree, true
	case "blob":
		return objBlob, true
	case "tag":
		return objTag, true
	}
	return 0, false
}

type gitObject struct {
	typ  objType
	data []byte
}

// hashObject computes the git object id: sha1("<type> <len>\0" + data).
func hashObject(typ objType, data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", typ.name(), len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// --- Commit / tree / tag views ---

type commitView struct {
	tree      string
	parents   []string
	author    string
	committer string
	message   string
}

func parseCommit(data []byte) *commitView {
	text := string(data)
	blank := strings.Index(text, "\n\n")
	head := text
	message := ""
	if blank >= 0 {
		head = text[:blank]
		message = text[blank+2:]
	}
	v := &commitView{}
	current := ""
	for _, line := range strings.Split(head, "\n") {
		if strings.HasPrefix(line, " ") && current != "" {
			continue
		}
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			current = ""
			continue
		}
		key := line[:sp]
		value := line[sp+1:]
		current = key
		switch key {
		case "tree":
			v.tree = value
		case "parent":
			v.parents = append(v.parents, value)
		case "author":
			v.author = value
		case "committer":
			v.committer = value
		}
	}
	v.message = message
	return v
}

type treeEntry struct {
	mode string
	name string
	oid  string
}

func parseTree(data []byte) []treeEntry {
	var entries []treeEntry
	off := 0
	for off < len(data) {
		sp := bytes.IndexByte(data[off:], ' ')
		if sp < 0 {
			break
		}
		sp += off
		nul := bytes.IndexByte(data[sp+1:], 0)
		if nul < 0 {
			break
		}
		nul += sp + 1
		if nul+21 > len(data) {
			break
		}
		entries = append(entries, treeEntry{
			mode: string(data[off:sp]),
			name: string(data[sp+1 : nul]),
			oid:  hex.EncodeToString(data[nul+1 : nul+21]),
		})
		off = nul + 21
	}
	return entries
}

func treeSortKey(mode, name string) string {
	if mode == "40000" {
		return name + "/"
	}
	return name
}

func buildTreeBytes(entries []treeEntry) []byte {
	sorted := make([]treeEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return treeSortKey(sorted[i].mode, sorted[i].name) < treeSortKey(sorted[j].mode, sorted[j].name)
	})
	var b []byte
	for _, e := range sorted {
		b = append(b, e.mode...)
		b = append(b, ' ')
		b = append(b, e.name...)
		b = append(b, 0)
		raw, _ := hex.DecodeString(e.oid)
		b = append(b, raw...)
	}
	return b
}

type tagView struct {
	object  string
	typ     objType
	tag     string
	tagger  string
	message string
}

func parseTag(data []byte) *tagView {
	text := string(data)
	blank := strings.Index(text, "\n\n")
	head := text
	message := ""
	if blank >= 0 {
		head = text[:blank]
		message = text[blank+2:]
	}
	v := &tagView{typ: objCommit}
	for _, line := range strings.Split(head, "\n") {
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		key := line[:sp]
		value := line[sp+1:]
		switch key {
		case "object":
			v.object = value
		case "type":
			if t, ok := typeFromName(value); ok {
				v.typ = t
			}
		case "tag":
			v.tag = value
		case "tagger":
			v.tagger = value
		}
	}
	v.message = message
	return v
}

func buildCommitBytes(tree string, parents []string, authorLine, committerLine, message, extra string) []byte {
	var sb strings.Builder
	sb.WriteString("tree " + tree + "\n")
	for _, p := range parents {
		sb.WriteString("parent " + p + "\n")
	}
	sb.WriteString("author " + authorLine + "\ncommitter " + committerLine + "\n")
	if extra != "" {
		sb.WriteString(extra)
	}
	sb.WriteString("\n")
	sb.WriteString(message)
	return []byte(sb.String())
}

func identLine(name, email string, epochSec int64) string {
	return name + " <" + email + "> " + fmt.Sprint(epochSec) + " +0000"
}

// splitIdent splits "Name <email> ts tz" into display parts.
func splitIdent(line string) (name, email, date string) {
	lt := strings.IndexByte(line, '<')
	gt := -1
	if lt >= 0 {
		if i := strings.IndexByte(line[lt+1:], '>'); i >= 0 {
			gt = lt + 1 + i
		}
	}
	if lt > 0 {
		name = line[:lt-1]
	} else {
		name = line
	}
	if lt >= 0 && gt >= 0 {
		email = line[lt+1 : gt]
	}
	date = fixedISO
	if gt >= 0 {
		rest := strings.Fields(strings.TrimSpace(line[gt+1:]))
		if len(rest) > 0 {
			if sec, err := strconvParseIntJS(rest[0]); err == nil {
				date = isoFromEpochSec(sec)
			}
		}
	}
	return name, email, date
}

// strconvParseIntJS parses a JS Number() of an integer token: plain
// decimal integers (the only form ident timestamps take).
func strconvParseIntJS(s string) (int64, error) {
	var n int64
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if i >= len(s) {
		return 0, errors.New("empty")
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int64(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

// --- pkt-line codec ---

func pkt(payload []byte) []byte {
	out := make([]byte, 4, 4+len(payload))
	hexLen := fmt.Sprintf("%04x", len(payload)+4)
	copy(out, hexLen)
	return append(out, payload...)
}

func pktString(s string) []byte { return pkt([]byte(s)) }

var (
	pktFlush       = []byte("0000")
	pktDelim       = []byte("0001")
	pktResponseEnd = []byte("0002")
)

type pktKind int

const (
	pktData pktKind = iota
	pktFlushKind
	pktDelimKind
	pktResponseEndKind
)

type pktItem struct {
	kind    pktKind
	payload []byte
}

type pktError struct{ msg string }

func (e *pktError) Error() string { return e.msg }

func parsePktStream(buf []byte, start int) ([]pktItem, int, error) {
	var items []pktItem
	off := start
	for off+4 <= len(buf) {
		lenText := string(buf[off : off+4])
		if !isHex4(lenText) {
			return nil, 0, &pktError{"bad pkt-line length " + JSONString(lenText)}
		}
		length, _ := strconvParseHex(lenText)
		switch length {
		case 0:
			items = append(items, pktItem{kind: pktFlushKind})
			return items, off + 4, nil
		case 1:
			items = append(items, pktItem{kind: pktDelimKind})
			return items, off + 4, nil
		case 2:
			items = append(items, pktItem{kind: pktResponseEndKind})
			return items, off + 4, nil
		}
		if length < 4 || off+length > len(buf) {
			return nil, 0, &pktError{"truncated pkt-line"}
		}
		items = append(items, pktItem{kind: pktData, payload: buf[off+4 : off+length]})
		off += length
	}
	if off != len(buf) {
		return nil, 0, &pktError{"trailing partial pkt-line"}
	}
	return items, off, nil
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if _, ok := hexVal(s[i]); !ok {
			return false
		}
	}
	return true
}

func strconvParseHex(s string) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		v, ok := hexVal(s[i])
		if !ok {
			return 0, errors.New("bad hex")
		}
		n = n*16 + int(v)
	}
	return n, nil
}

func splitPktAndRest(buf []byte) ([][]byte, []byte, error) {
	var payloads [][]byte
	off := 0
	for {
		items, next, err := parsePktStream(buf, off)
		if err != nil {
			return nil, nil, err
		}
		for _, it := range items {
			if it.kind == pktData {
				payloads = append(payloads, it.payload)
			}
		}
		off = next
		if len(items) > 0 && items[len(items)-1].kind == pktFlushKind {
			return payloads, buf[off:], nil
		}
		if off >= len(buf) {
			return payloads, []byte{}, nil
		}
	}
}

func pktTextLines(lines []string) []byte {
	var b []byte
	for _, l := range lines {
		b = append(b, pktString(l)...)
	}
	return b
}

// sidebandWrap wraps raw bytes in sideband band-1 packets.
func sidebandWrap(data []byte) []byte {
	const chunkSize = 32768
	var b []byte
	for off := 0; off < len(data); off += chunkSize {
		end := off + chunkSize
		if end > len(data) {
			end = len(data)
		}
		framed := make([]byte, 0, 1+end-off)
		framed = append(framed, 1)
		framed = append(framed, data[off:end]...)
		b = append(b, pkt(framed)...)
	}
	if len(data) == 0 {
		b = append(b, pkt([]byte{1})...)
	}
	return b
}

// --- Pack codec ---

type packError struct{ msg string }

func (e *packError) Error() string { return e.msg }

func readPackVarint(buf []byte, off int) (value int, next int) {
	value = int(buf[off] & 0x0f)
	shift := uint(4)
	pos := off + 1
	msb := buf[off] & 0x80
	for msb != 0 {
		value |= int(buf[pos]&0x7f) << shift
		shift += 7
		msb = buf[pos] & 0x80
		pos++
	}
	return value, pos
}

func readOfsVarint(buf []byte, off int) (value int, next int) {
	value = int(buf[off] & 0x7f)
	pos := off + 1
	for buf[pos-1]&0x80 != 0 {
		value = ((value + 1) << 7) | int(buf[pos]&0x7f)
		pos++
	}
	return value, pos
}

func readDeltaVarint(buf []byte, off int) (value int, next int) {
	value = 0
	shift := uint(0)
	pos := off
	for {
		b := buf[pos]
		pos++
		value |= int(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	return value, pos
}

func applyDelta(base, delta []byte) ([]byte, error) {
	srcSize, p1 := readDeltaVarint(delta, 0)
	if srcSize != len(base) {
		return nil, &packError{"delta source size mismatch"}
	}
	dstSize, pos := readDeltaVarint(delta, p1)
	out := make([]byte, dstSize)
	outPos := 0
	for pos < len(delta) {
		op := delta[pos]
		pos++
		if op&0x80 != 0 {
			cpOff := 0
			cpSize := 0
			for i := 0; i < 4; i++ {
				if op&(1<<uint(i)) != 0 {
					cpOff |= int(delta[pos]) << (uint(i) * 8)
					pos++
				}
			}
			for i := 0; i < 3; i++ {
				if op&(0x10<<uint(i)) != 0 {
					cpSize |= int(delta[pos]) << (uint(i) * 8)
					pos++
				}
			}
			if cpSize == 0 {
				cpSize = 0x10000
			}
			if cpOff+cpSize > len(base) || outPos+cpSize > len(out) {
				return nil, &packError{"delta copy out of range"}
			}
			copy(out[outPos:], base[cpOff:cpOff+cpSize])
			outPos += cpSize
		} else if op != 0 {
			if pos+int(op) > len(delta) || outPos+int(op) > len(out) {
				return nil, &packError{"delta insert out of range"}
			}
			copy(out[outPos:], delta[pos:pos+int(op)])
			pos += int(op)
			outPos += int(op)
		} else {
			return nil, &packError{"delta opcode 0"}
		}
	}
	if outPos != dstSize {
		return nil, &packError{"delta result size mismatch"}
	}
	return out, nil
}

// inflateAt inflates one zlib stream starting at off, returning the
// inflated bytes and the exact number of input bytes consumed.
func inflateAt(buf []byte, off int) ([]byte, int, error) {
	r := bytes.NewReader(buf[off:])
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, 0, &packError{err.Error()}
	}
	data, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return nil, 0, &packError{err.Error()}
	}
	consumed := len(buf) - off - r.Len()
	return data, consumed, nil
}

// readPack parses a full pack. lookup resolves base oids already in the
// object store. Returns new objects only, keyed by oid.
func readPack(buf []byte, lookup func(oid string) *gitObject) (map[string]*gitObject, error) {
	if len(buf) < 12 {
		return nil, &packError{"pack too short"}
	}
	if binary.BigEndian.Uint32(buf[0:4]) != 0x5041434b {
		return nil, &packError{"bad pack signature"}
	}
	version := binary.BigEndian.Uint32(buf[4:8])
	if version != 2 && version != 3 {
		return nil, &packError{fmt.Sprintf("unsupported pack version %d", version)}
	}
	count := binary.BigEndian.Uint32(buf[8:12])
	if len(buf) >= 32 {
		h := sha1.New()
		h.Write(buf[:len(buf)-20])
		if !bytes.Equal(h.Sum(nil), buf[len(buf)-20:]) {
			return nil, &packError{"pack checksum mismatch"}
		}
	}
	end := len(buf) - 20
	found := map[string]*gitObject{}
	byOffset := map[int]*gitObject{}
	type pendingDelta struct {
		offset     int
		typ        int
		baseOffset int
		baseOID    string
		delta      []byte
	}
	var pending []pendingDelta
	off := 12
	for i := uint32(0); i < count; i++ {
		if off >= end {
			return nil, &packError{"pack truncated"}
		}
		entryOffset := off
		first := buf[off]
		typeNum := (first >> 4) & 0x07
		declaredSize, next := readPackVarint(buf, off)
		off = next
		if typeNum == 6 {
			ofs, next2 := readOfsVarint(buf, off)
			off = next2
			data, consumed, err := inflateAt(buf, off)
			if err != nil {
				return nil, err
			}
			off += consumed
			pending = append(pending, pendingDelta{offset: entryOffset, typ: 6, baseOffset: entryOffset - ofs, delta: data})
		} else if typeNum == 7 {
			if off+20 > end {
				return nil, &packError{"ref-delta base truncated"}
			}
			baseOID := hex.EncodeToString(buf[off : off+20])
			off += 20
			data, consumed, err := inflateAt(buf, off)
			if err != nil {
				return nil, err
			}
			off += consumed
			pending = append(pending, pendingDelta{offset: entryOffset, typ: 7, baseOffset: -1, baseOID: baseOID, delta: data})
		} else {
			typ, ok := codeType(int(typeNum))
			if !ok {
				return nil, &packError{fmt.Sprintf("unknown pack object type %d", typeNum)}
			}
			data, consumed, err := inflateAt(buf, off)
			if err != nil {
				return nil, err
			}
			off += consumed
			if len(data) != declaredSize {
				return nil, &packError{"object size mismatch"}
			}
			obj := &gitObject{typ: typ, data: data}
			oid := hashObject(typ, data)
			byOffset[entryOffset] = obj
			found[oid] = obj
		}
	}
	guard := len(pending) + 2
	for len(pending) > 0 {
		if guard <= 0 {
			return nil, &packError{"delta chain too deep"}
		}
		guard--
		progressed := false
		for i := len(pending) - 1; i >= 0; i-- {
			p := pending[i]
			var base *gitObject
			if p.typ == 6 {
				base = byOffset[p.baseOffset]
			} else {
				base = lookup(p.baseOID)
				if base == nil {
					base = found[p.baseOID]
				}
				if base == nil {
					for _, obj := range byOffset {
						if hashObject(obj.typ, obj.data) == p.baseOID {
							base = obj
							break
						}
					}
				}
			}
			if base == nil {
				continue
			}
			data, err := applyDelta(base.data, p.delta)
			if err != nil {
				return nil, err
			}
			obj := &gitObject{typ: base.typ, data: data}
			oid := hashObject(obj.typ, obj.data)
			byOffset[p.offset] = obj
			found[oid] = obj
			pending = append(pending[:i], pending[i+1:]...)
			progressed = true
		}
		if !progressed {
			return nil, &packError{"unresolvable delta base"}
		}
	}
	return found, nil
}

func codeType(code int) (objType, bool) {
	switch code {
	case 1:
		return objCommit, true
	case 2:
		return objTree, true
	case 3:
		return objBlob, true
	case 4:
		return objTag, true
	}
	return 0, false
}

// Pooled zlib writers for pack writing: creating a writer per object is
// the dominant cost of writePack on small objects.
var deflatePool = sync.Pool{
	New: func() interface{} {
		var dummy bytes.Buffer
		w, _ := zlib.NewWriterLevel(&dummy, zlib.DefaultCompression)
		return w
	},
}

func deflateBytes(data []byte) []byte {
	var buf bytes.Buffer
	w := deflatePool.Get().(*zlib.Writer)
	w.Reset(&buf)
	w.Write(data)
	w.Close()
	deflatePool.Put(w)
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out
}

// writePack writes a plain pack (no deltas): header, one zlib entry per
// object, SHA-1 trailer.
func writePack(objects []*gitObject) []byte {
	var body []byte
	header := make([]byte, 12)
	copy(header[0:4], "PACK")
	binary.BigEndian.PutUint32(header[4:8], 2)
	binary.BigEndian.PutUint32(header[8:12], uint32(len(objects)))
	body = append(body, header...)
	for _, obj := range objects {
		code := int(obj.typ)
		size := len(obj.data)
		var first []byte
		remaining := size
		b := byte((code << 4) | (remaining & 0x0f))
		remaining >>= 4
		if remaining > 0 {
			b |= 0x80
		}
		first = append(first, b)
		for remaining > 0 {
			b = byte(remaining & 0x7f)
			remaining >>= 7
			if remaining > 0 {
				b |= 0x80
			}
			first = append(first, b)
		}
		body = append(body, first...)
		body = append(body, deflateBytes(obj.data)...)
	}
	h := sha1.New()
	h.Write(body)
	return append(body, h.Sum(nil)...)
}
