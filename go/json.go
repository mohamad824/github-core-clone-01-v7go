// JSON values with insertion-ordered objects and JavaScript-compatible
// encoding: this forge's wire bytes must match JSON.stringify exactly
// (key order, string escaping, number formatting), because the checks
// compare parsed values while the byte-identity checks compare raw bytes.

package main

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JVal is a JSON value: nil, bool, string, int64, float64, []JVal, *JObj.
type JVal interface{}

type jpair struct {
	k string
	v JVal
}

// JObj is an ordered JSON object. Set replaces an existing key in place
// (matching JS object assignment, which keeps the original position).
type JObj struct {
	pairs []jpair
}

func NewObj(pairs ...jpair) *JObj {
	o := &JObj{}
	for _, p := range pairs {
		o.Set(p.k, p.v)
	}
	return o
}

func KV(k string, v JVal) jpair { return jpair{k, v} }

func (o *JObj) Set(k string, v JVal) {
	for i := range o.pairs {
		if o.pairs[i].k == k {
			o.pairs[i].v = v
			return
		}
	}
	o.pairs = append(o.pairs, jpair{k, v})
}

func (o *JObj) Get(k string) (JVal, bool) {
	for i := range o.pairs {
		if o.pairs[i].k == k {
			return o.pairs[i].v, true
		}
	}
	return nil, false
}

// MarshalJSON renders the value exactly like JSON.stringify.
func MarshalJSON(v JVal) []byte {
	var b []byte
	b = appendJSON(b, v)
	return b
}

func appendJSON(b []byte, v JVal) []byte {
	switch t := v.(type) {
	case nil:
		return append(b, "null"...)
	case bool:
		if t {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case string:
		return appendJSONString(b, t)
	case int:
		return strconv.AppendInt(b, int64(t), 10)
	case int64:
		return strconv.AppendInt(b, t, 10)
	case float64:
		return append(b, jsFloat(t)...)
	case float32:
		return append(b, jsFloat(float64(t))...)
	case []JVal:
		b = append(b, '[')
		for i, e := range t {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSON(b, e)
		}
		return append(b, ']')
	case *JObj:
		b = append(b, '{')
		for i, p := range t.pairs {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, p.k)
			b = append(b, ':')
			b = appendJSON(b, p.v)
		}
		return append(b, '}')
	case json.Number:
		// Only produced by request parsing; echo the literal.
		return append(b, string(t)...)
	default:
		// Programmer error: never emit invalid JSON.
		return append(b, "null"...)
	}
}

const hexDigits = "0123456789abcdef"

// appendJSONString escapes exactly like JSON.stringify: quote, backslash,
// the five short control escapes, other C0 controls as \u00xx (lowercase
// hex); everything else is emitted as UTF-8, with invalid bytes replaced
// by U+FFFD the way JS string conversion does.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			if start < i {
				b = append(b, s[start:i]...)
			}
			switch c {
			case '"':
				b = append(b, '\\', '"')
			case '\\':
				b = append(b, '\\', '\\')
			case '\b':
				b = append(b, '\\', 'b')
			case '\f':
				b = append(b, '\\', 'f')
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
			}
			i++
			start = i
			continue
		}
		if c < utf8.RuneSelf {
			i++
			continue
		}
		// Multi-byte rune: validate; replace invalid bytes with U+FFFD.
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if start < i {
				b = append(b, s[start:i]...)
			}
			b = append(b, 0xef, 0xbf, 0xbd) // U+FFFD
			i++
			start = i
			continue
		}
		i += size
	}
	if start < len(s) {
		b = append(b, s[start:]...)
	}
	return append(b, '"')
}

// JSONString returns the JSON.stringify form of one string.
func JSONString(s string) string {
	return string(appendJSONString(nil, s))
}

// jsFloat formats a float64 the way JS Number.prototype.toString /
// JSON.stringify does: shortest round-trip decimal in the [1e-6, 1e21)
// range, exponential notation outside it, exponent without zero padding.
func jsFloat(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// JSON.stringify(NaN/Infinity) === "null".
		return "null"
	}
	if f == 0 {
		return "0"
	}
	af := math.Abs(f)
	if af >= 1e-6 && af < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// Go: "1.5e+21" / "1e-07". JS: "1.5e+21" / "1e-7".
	// Strip a leading zero in the exponent.
	if i := strings.IndexByte(s, 'e'); i >= 0 && i+2 < len(s) && s[i+2] == '0' {
		s = s[:i+2] + s[i+3:]
	}
	return s
}

// jsNumber converts a parsed JSON value to a float64 the way JS Number()
// does, reporting whether the result is a finite number. Strings follow
// JS: surrounding whitespace trimmed, then decimal/hex/binary/octal/
// Infinity forms; "" -> 0; anything else -> NaN.
func jsNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, true // Number(null) === 0
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case float64:
		return t, !math.IsNaN(t) && !math.IsInf(t, 0)
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	case string:
		return jsParseNumberString(t)
	default:
		return math.NaN(), false
	}
}

func jsParseNumberString(s string) (float64, bool) {
	t := strings.Trim(s, " \t\n\r\v\f\u00a0\ufeff\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u3000")
	if t == "" {
		return 0, true
	}
	if t == "Infinity" || t == "+Infinity" {
		return math.Inf(1), false
	}
	if t == "-Infinity" {
		return math.Inf(-1), false
	}
	// Hex / binary / octal integer literals.
	rest := t
	neg := false
	if len(rest) > 0 && (rest[0] == '+' || rest[0] == '-') {
		neg = rest[0] == '-'
		rest = rest[1:]
	}
	if len(rest) > 2 && rest[0] == '0' {
		switch rest[1] {
		case 'x', 'X':
			n, err := strconv.ParseUint(rest[2:], 16, 64)
			if err != nil {
				return math.NaN(), false
			}
			f := float64(n)
			if neg {
				f = -f
			}
			return f, true
		case 'b', 'B':
			n, err := strconv.ParseUint(rest[2:], 2, 64)
			if err != nil {
				return math.NaN(), false
			}
			f := float64(n)
			if neg {
				f = -f
			}
			return f, true
		case 'o', 'O':
			n, err := strconv.ParseUint(rest[2:], 8, 64)
			if err != nil {
				return math.NaN(), false
			}
			f := float64(n)
			if neg {
				f = -f
			}
			return f, true
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return math.NaN(), false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f, false
	}
	return f, true
}

// jsParseInt mirrors JS parseInt(s, 10): leading whitespace skipped,
// optional sign, then decimal digits; no digits -> NaN (ok=false).
func jsParseInt(s string) (int64, bool) {
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
			i++
			continue
		}
		break
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	var n int64
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int64(s[i]-'0')
		if n > 1<<53 {
			// JS would keep going in float; clamp — values this large
			// never decide a check (page/per_page bounds).
			n = 1 << 53
		}
		i++
	}
	if i == start {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// jsStringOfNumber mirrors JS String(n) for a finite float64.
func jsStringOfNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatInt(int64(f), 10)
	}
	return jsFloat(f)
}

// --- Request-body JSON parsing (encoding/json into interface{}) ---

// parseJSONStrict rejects trailing non-whitespace data, matching JSON.parse.
func parseJSONStrict(body []byte) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func isRecord(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func asString(v interface{}, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

func asBool(v interface{}, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}
