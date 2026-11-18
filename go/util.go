// Startup configuration, deterministic time, scenario derivation, URL
// query handling, and small shared helpers. All timestamps derive from a
// single startup constant; nothing here reads the wall clock per request.

package main

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const zeroOID = "0000000000000000000000000000000000000000"
const forgeAgent = "forge/1.0"

// --- Civil-date conversion (Howard Hinnant's algorithm) ---

func civilFromDays(z int64) (int64, int64, int64) {
	zz := z + 719468
	era := floorDiv(zz, 146097)
	doe := zz - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

func pad4(n int64) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func isoFromEpochSec(sec int64) string {
	days := floorDiv(sec, 86400)
	rem := sec - days*86400
	hh := rem / 3600
	mm := (rem - hh*3600) / 60
	ss := rem - hh*3600 - mm*60
	y, mo, d := civilFromDays(days)
	return pad4(y) + "-" + pad2(mo) + "-" + pad2(d) + "T" + pad2(hh) + ":" + pad2(mm) + ":" + pad2(ss) + "Z"
}

func daysFromCivil(y, m, d int64) int64 {
	yy := y
	if m <= 2 {
		yy = y - 1
	}
	era := floorDiv(yy, 400)
	yoe := yy - era*400
	mp := m - 3
	if m <= 2 {
		mp = m + 9
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func parseTrialNow(text string) int64 {
	const fallback = 1735689600 // 2025-01-01T00:00:00Z
	t := strings.TrimSpace(text)
	if t == "" {
		return fallback
	}
	if isAllDigits(t) {
		ms, err := strconv.ParseInt(t, 10, 64)
		if err == nil && ms > 0 {
			return ms / 1000
		}
		return fallback
	}
	// YYYY-MM-DD[T ]HH:MM:SS[.frac](Z|±HH:?MM)?
	if len(t) < 19 {
		return fallback
	}
	y, ok1 := atoi2(t[0:4])
	mo, ok2 := atoi2(t[5:7])
	d, ok3 := atoi2(t[8:10])
	hh, ok4 := atoi2(t[11:13])
	mm, ok5 := atoi2(t[14:16])
	ss, ok6 := atoi2(t[17:19])
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 ||
		t[4] != '-' || t[7] != '-' || (t[10] != 'T' && t[10] != ' ') || t[13] != ':' || t[16] != ':' {
		return fallback
	}
	sec := daysFromCivil(y, mo, d)*86400 + hh*3600 + mm*60 + ss
	rest := t[19:]
	if strings.HasPrefix(rest, ".") {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		rest = rest[i:]
	}
	if rest == "" || rest == "Z" {
		return sec
	}
	if len(rest) >= 3 && (rest[0] == '+' || rest[0] == '-') {
		sign := int64(1)
		if rest[0] == '-' {
			sign = -1
		}
		digits := strings.Replace(rest[1:], ":", "", 1)
		if len(digits) == 4 {
			oh, okA := atoi2(digits[0:2])
			om, okB := atoi2(digits[2:4])
			if okA && okB {
				sec -= sign * (oh*3600 + om*60)
				return sec
			}
		}
		return fallback
	}
	return fallback
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func atoi2(s string) (int64, bool) {
	if len(s) == 0 {
		return 0, false
	}
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n, true
}

// --- Scenario derivation (same published algorithm as the suite) ---

func fnv1a64(text string) uint64 {
	var hash uint64 = 0xcbf29ce484222325
	for i := 0; i < len(text); i++ {
		hash ^= uint64(text[i])
		hash *= 0x100000001b3
	}
	return hash
}

func mix64(value uint64) uint64 {
	z := value + 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

const scenarioSeed = uint64(0x6a09e667f3bcc909)

func scenarioSeedFor(stream string) uint64 {
	return mix64(scenarioSeed ^ fnv1a64(stream))
}

type rng struct{ state uint64 }

func newRng(seed uint64) *rng {
	if seed == 0 {
		seed = scenarioSeed
	}
	return &rng{state: seed}
}

func (r *rng) nextU64() uint64 {
	x := r.state
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.state = x
	return x * 0x2545f4914f6cdd1d
}

func (r *rng) nextU32() uint32 {
	return uint32(r.nextU64() >> 32)
}

func (r *rng) nextInt(max int) int {
	const range_ = 4294967296
	limit := range_ - (range_ % uint64(max))
	v := uint64(r.nextU32())
	for v >= limit {
		v = uint64(r.nextU32())
	}
	return int(v % uint64(max))
}

var firstNames = []string{
	"ada", "bram", "cora", "dmitri", "elena", "felix", "greta", "hugo", "iris", "jonas", "kira", "lars",
	"mira", "nico", "olga", "pavel", "quinn", "rosa", "sven", "tess", "ulla", "vito", "wren", "yara", "zane",
}
var lastNames = []string{
	"anders", "bauer", "costa", "dahl", "eriksen", "fischer", "garcia", "holm", "iversen", "jansen", "keller",
	"lind", "moreau", "novak", "olsen", "peters", "quist", "rossi", "sato", "turi", "ulrich", "vega", "weber",
	"xu", "yilmaz", "zimmer",
}
var repoAdjectives = []string{
	"amber", "brisk", "calm", "crisp", "eager", "faint", "gentle", "happy", "idle", "jolly", "keen", "lively",
	"mellow", "nimble", "open", "plain", "quick", "quiet", "rapid", "soft", "steady", "tidy", "upbeat",
	"vivid", "warm", "witty", "young", "zesty",
}
var repoNouns = []string{
	"acorn", "badger", "canyon", "cedar", "comet", "coral", "delta", "ember", "falcon", "glade", "harbor",
	"indigo", "juniper", "kettle", "lantern", "meadow", "nebula", "orchid", "pebble", "quartz", "raven",
	"saffron", "thistle", "umber", "violet", "willow", "yarrow", "zephyr",
}

type scenarioUser struct {
	id    int
	login string
	name  string
	email string
}

type scenario struct {
	viewer   scenarioUser
	teammate scenarioUser
	stranger scenarioUser
	org      string
}

func deriveScenario() scenario {
	r := newRng(scenarioSeedFor("users"))
	used := map[string]bool{}
	users := make([]scenarioUser, 0, 12)
	for i := 0; i < 12; i++ {
		first := firstNames[r.nextInt(len(firstNames))]
		last := lastNames[r.nextInt(len(lastNames))]
		base := first + last // already [a-z0-9-]
		login := base
		suffix := 2
		for used[login] {
			login = base + "-" + strconv.Itoa(suffix)
			suffix++
		}
		used[login] = true
		users = append(users, scenarioUser{id: 1000 + i, login: login, name: first + " " + last, email: login + "@example.invalid"})
	}
	orgRng := newRng(scenarioSeedFor("org"))
	org := repoAdjectives[orgRng.nextInt(len(repoAdjectives))] + "-" + repoNouns[orgRng.nextInt(len(repoNouns))] + "-org"
	return scenario{viewer: users[0], teammate: users[1], stranger: users[2], org: org}
}

// --- Globals fixed at startup ---

var (
	fixedEpochSec int64
	fixedISO      string
	fixedISOJSON  string
	expectedToken string
	scen          scenario
	viewerLogin   string
)

func initConfig() {
	fixedEpochSec = parseTrialNow(os.Getenv("MAKEFUN_TRIAL_NOW"))
	fixedISO = isoFromEpochSec(fixedEpochSec)
	fixedISOJSON = JSONString(fixedISO)
	expectedToken = os.Getenv("MAKEFUN_SHOP_TOKEN")
	scen = deriveScenario()
	viewerLogin = scen.viewer.login
}

// --- Global ids ---

func globalID(kind string, parts ...interface{}) string {
	var sb strings.Builder
	sb.WriteString(kind)
	for _, p := range parts {
		sb.WriteByte(':')
		switch t := p.(type) {
		case string:
			sb.WriteString(t)
		case int:
			sb.WriteString(strconv.Itoa(t))
		case int64:
			sb.WriteString(strconv.FormatInt(t, 10))
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(sb.String()))
}

func decodeGlobalID(id string) (kind string, parts []string, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(id)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(id)
		if err != nil {
			return "", nil, false
		}
	}
	parts = strings.Split(string(raw), ":")
	if len(parts) < 2 {
		return "", nil, false
	}
	return parts[0], parts[1:], true
}

// --- URL query strings (URLSearchParams semantics) ---

type qpair struct {
	k, v string
}

type query struct {
	pairs []qpair
}

func parseQuery(s string) *query {
	q := &query{}
	if s == "" {
		return q
	}
	for _, seg := range strings.Split(s, "&") {
		if seg == "" {
			continue
		}
		k, v := seg, ""
		if i := strings.IndexByte(seg, '='); i >= 0 {
			k, v = seg[:i], seg[i+1:]
		}
		q.pairs = append(q.pairs, qpair{formDecode(k), formDecode(v)})
	}
	return q
}

func (q *query) clone() *query {
	nq := &query{pairs: make([]qpair, len(q.pairs))}
	copy(nq.pairs, q.pairs)
	return nq
}

// get returns the first value for k.
func (q *query) get(k string) (string, bool) {
	for _, p := range q.pairs {
		if p.k == k {
			return p.v, true
		}
	}
	return "", false
}

func (q *query) getOr(k, def string) string {
	if v, ok := q.get(k); ok {
		return v
	}
	return def
}

// set replaces the first occurrence's value and removes the rest, or
// appends when absent (URLSearchParams.set semantics).
func (q *query) set(k, v string) {
	out := q.pairs[:0]
	placed := false
	for _, p := range q.pairs {
		if p.k == k {
			if !placed {
				out = append(out, qpair{k, v})
				placed = true
			}
			continue
		}
		out = append(out, p)
	}
	if !placed {
		out = append(out, qpair{k, v})
	}
	q.pairs = out
}

func (q *query) String() string {
	var sb strings.Builder
	for i, p := range q.pairs {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(formEncode(p.k))
		sb.WriteByte('=')
		sb.WriteString(formEncode(p.v))
	}
	return sb.String()
}

// formDecode: '+' -> space, lenient %-decoding (bad escapes kept literal).
func formDecode(s string) string {
	if !strings.ContainsAny(s, "+%") {
		return s
	}
	var b []byte
	for i := 0; i < len(s); {
		c := s[i]
		if c == '+' {
			b = append(b, ' ')
			i++
			continue
		}
		if c == '%' && i+2 < len(s)+0 && i+2 <= len(s)-1 {
			hi, ok1 := hexVal(s[i+1])
			lo, ok2 := hexVal(s[i+2])
			if ok1 && ok2 {
				b = append(b, hi<<4|lo)
				i += 3
				continue
			}
		}
		b = append(b, c)
		i++
	}
	return string(b)
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func formEncode(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '*' || c == '-' || c == '.' || c == '_':
			sb.WriteByte(c)
		case c == ' ':
			sb.WriteByte('+')
		default:
			sb.WriteByte('%')
			sb.WriteByte("0123456789ABCDEF"[c>>4])
			sb.WriteByte("0123456789ABCDEF"[c&0x0f])
		}
	}
	return sb.String()
}

// decodeSegment mirrors decodeURIComponent: strict %-decoding, and the
// result must be valid UTF-8, else an error (the router answers 400).
func decodeSegment(s string) (string, bool) {
	if !strings.Contains(s, "%") {
		return s, true
	}
	var b []byte
	for i := 0; i < len(s); {
		c := s[i]
		if c == '%' {
			if i+2 >= len(s) {
				return "", false
			}
			hi, ok1 := hexVal(s[i+1])
			lo, ok2 := hexVal(s[i+2])
			if !ok1 || !ok2 {
				return "", false
			}
			b = append(b, hi<<4|lo)
			i += 3
			continue
		}
		b = append(b, c)
		i++
	}
	if !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

// isOID reports whether s is a 40-char lowercase hex object id.
func isOID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < 40; i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// lenientB64 decodes like Node's Buffer.from(s, 'base64'): characters
// outside the alphabet are ignored, missing padding is tolerated, and
// decoding never fails.
func lenientB64(s string) []byte {
	var clean []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' {
			clean = append(clean, c)
		}
	}
	// Drop a trailing partial quantum of length 1 mod 4, which cannot decode.
	for len(clean)%4 == 1 {
		clean = clean[:len(clean)-1]
	}
	for len(clean)%4 != 0 {
		clean = append(clean, '=')
	}
	out, err := base64.StdEncoding.DecodeString(string(clean))
	if err != nil {
		return []byte{}
	}
	return out
}
