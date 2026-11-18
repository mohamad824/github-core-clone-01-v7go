// GraphQL: hand-written lexer and recursive-descent parser for the
// document subset the suite sends (operations, fragments, variables,
// aliases, input objects, lists, enums, @skip/@include).

package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type gqlSyntaxError struct{ msg string }

func (e *gqlSyntaxError) Error() string { return e.msg }

type gqlToken struct {
	kind  string // name,int,float,string,punct,eof
	value string
}

func lexGraphQL(src string) ([]gqlToken, error) {
	var toks []gqlToken
	i := 0
	for i < len(src) {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			i++
			continue
		}
		if c == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '.' {
			if strings.HasPrefix(src[i:], "...") {
				toks = append(toks, gqlToken{"punct", "..."})
				i += 3
				continue
			}
			return nil, &gqlSyntaxError{"Syntax Error: unexpected dot"}
		}
		if strings.IndexByte("{}()[]:!=@$&|", c) >= 0 {
			toks = append(toks, gqlToken{"punct", string(c)})
			i++
			continue
		}
		if c == '"' {
			if strings.HasPrefix(src[i:], `"""`) {
				end := strings.Index(src[i+3:], `"""`)
				if end < 0 {
					return nil, &gqlSyntaxError{"Syntax Error: unterminated block string"}
				}
				raw := src[i+3 : i+3+end]
				if strings.HasPrefix(raw, "\n") {
					raw = raw[1:]
				}
				if strings.HasSuffix(raw, "\n") {
					raw = raw[:len(raw)-1]
				}
				toks = append(toks, gqlToken{"string", raw})
				i = i + 3 + end + 3
				continue
			}
			j := i + 1
			var sb strings.Builder
			closed := false
			for j < len(src) && src[j] != '"' {
				if src[j] == '\n' {
					return nil, &gqlSyntaxError{"Syntax Error: unterminated string"}
				}
				if src[j] == '\\' {
					if j+1 >= len(src) {
						return nil, &gqlSyntaxError{"Syntax Error: unterminated string"}
					}
					esc := src[j+1]
					switch esc {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					case 'b':
						sb.WriteByte('\b')
					case 'f':
						sb.WriteByte('\f')
					case 'u':
						if j+5 >= len(src) {
							return nil, &gqlSyntaxError{"Syntax Error: unterminated string"}
						}
						unit, err := strconv.ParseUint(src[j+2:j+6], 16, 32)
						if err != nil {
							return nil, &gqlSyntaxError{"Syntax Error: invalid unicode escape"}
						}
						j += 4
						// Surrogate pair?
						if unit >= 0xD800 && unit <= 0xDBFF && j+7 < len(src) && src[j+2] == '\\' && src[j+3] == 'u' {
							lo, err2 := strconv.ParseUint(src[j+4:j+8], 16, 32)
							if err2 == nil && lo >= 0xDC00 && lo <= 0xDFFF {
								sb.WriteRune(0x10000 + (rune(unit)-0xD800)<<10 + (rune(lo) - 0xDC00))
								j += 6
								break
							}
						}
						if unit >= 0xD800 && unit <= 0xDFFF {
							sb.WriteRune(utf8.RuneError)
						} else {
							sb.WriteRune(rune(unit))
						}
					case '/', '\\', '"':
						sb.WriteByte(esc)
					default:
						return nil, &gqlSyntaxError{fmt.Sprintf("Syntax Error: invalid escape \\%c", esc)}
					}
					j += 2
				} else {
					sb.WriteByte(src[j])
					j++
				}
			}
			if j < len(src) {
				closed = true
			}
			if !closed {
				return nil, &gqlSyntaxError{"Syntax Error: unterminated string"}
			}
			toks = append(toks, gqlToken{"string", sb.String()})
			i = j + 1
			continue
		}
		if c == '-' || (c >= '0' && c <= '9') {
			j := i
			if src[j] == '-' {
				j++
			}
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			isFloat := false
			if j+1 < len(src) && src[j] == '.' && src[j+1] >= '0' && src[j+1] <= '9' {
				isFloat = true
				j++
				for j < len(src) && src[j] >= '0' && src[j] <= '9' {
					j++
				}
			}
			if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
				isFloat = true
				j++
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				for j < len(src) && src[j] >= '0' && src[j] <= '9' {
					j++
				}
			}
			kind := "int"
			if isFloat {
				kind = "float"
			}
			toks = append(toks, gqlToken{kind, src[i:j]})
			i = j
			continue
		}
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' {
			j := i
			for j < len(src) && (src[j] >= 'A' && src[j] <= 'Z' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= '0' && src[j] <= '9' || src[j] == '_') {
				j++
			}
			toks = append(toks, gqlToken{"name", src[i:j]})
			i = j
			continue
		}
		return nil, &gqlSyntaxError{fmt.Sprintf("Syntax Error: unexpected character %s", JSONString(string(c)))}
	}
	toks = append(toks, gqlToken{"eof", ""})
	return toks, nil
}

// --- AST ---

type gqlValue struct {
	kind     string // variable,int,float,string,boolean,null,enum,list,object
	name     string
	intVal   int64
	floatVal float64
	strVal   string
	boolVal  bool
	items    []*gqlValue
	fields   []gqlObjField
}

type gqlObjField struct {
	name  string
	value *gqlValue
}

type gqlType struct {
	kind string // named,list,nonNull
	name string
	of   *gqlType
}

type gqlDirective struct {
	name string
	args map[string]*gqlValue
}

type gqlSel struct {
	kind      string // field,spread,inline
	alias     string
	hasAlias  bool
	name      string
	args      map[string]*gqlValue
	dirs      []gqlDirective
	selection []*gqlSel
	fragName  string
	typeCond  string
	hasCond   bool
}

type gqlVarDef struct {
	name string
	typ  *gqlType
	def  *gqlValue // nil = no default
}

type gqlDefinition struct {
	kind      string // operation,fragment
	opType    string // query,mutation,subscription
	name      string
	varDefs   []gqlVarDef
	dirs      []gqlDirective
	selection []*gqlSel
	typeCond  string
}

// --- Parser ---

type gqlParser struct {
	toks []gqlToken
	pos  int
}

func newGqlParser(src string) (*gqlParser, error) {
	toks, err := lexGraphQL(src)
	if err != nil {
		return nil, err
	}
	return &gqlParser{toks: toks}, nil
}

func (p *gqlParser) peek() gqlToken { return p.toks[p.pos] }
func (p *gqlParser) next() gqlToken { t := p.toks[p.pos]; p.pos++; return t }

func (p *gqlParser) expectPunct(v string) error {
	t := p.next()
	if t.kind != "punct" || t.value != v {
		got := JSONString(t.value)
		if t.kind == "eof" {
			got = "end of document"
		}
		return &gqlSyntaxError{fmt.Sprintf("Syntax Error: expected %q but got %s", v, got)}
	}
	return nil
}

func (p *gqlParser) acceptPunct(v string) bool {
	t := p.peek()
	if t.kind == "punct" && t.value == v {
		p.pos++
		return true
	}
	return false
}

func (p *gqlParser) expectName() (string, error) {
	t := p.next()
	if t.kind != "name" {
		return "", &gqlSyntaxError{fmt.Sprintf("Syntax Error: expected Name but got %s", JSONString(t.value))}
	}
	return t.value, nil
}

func (p *gqlParser) peekName(v string) bool {
	t := p.peek()
	return t.kind == "name" && t.value == v
}

func (p *gqlParser) parseDocument() ([]*gqlDefinition, error) {
	var defs []*gqlDefinition
	for p.peek().kind != "eof" {
		d, err := p.parseDefinition()
		if err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	return defs, nil
}

func (p *gqlParser) parseDefinition() (*gqlDefinition, error) {
	t := p.peek()
	if t.kind == "punct" && t.value == "{" {
		sel, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		return &gqlDefinition{kind: "operation", opType: "query", selection: sel}, nil
	}
	if t.kind == "name" && t.value == "fragment" {
		p.next()
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if name == "on" {
			return nil, &gqlSyntaxError{`Syntax Error: fragment name cannot be "on"`}
		}
		if !p.peekName("on") {
			return nil, &gqlSyntaxError{`Syntax Error: expected "on" after fragment name`}
		}
		p.next()
		typeCond, err := p.expectName()
		if err != nil {
			return nil, err
		}
		dirs, err := p.parseDirectives()
		if err != nil {
			return nil, err
		}
		sel, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		return &gqlDefinition{kind: "fragment", name: name, typeCond: typeCond, dirs: dirs, selection: sel}, nil
	}
	if t.kind == "name" && (t.value == "query" || t.value == "mutation" || t.value == "subscription") {
		opType := p.next().value
		name := ""
		if nt := p.peek(); nt.kind == "name" {
			name = p.next().value
		}
		varDefs, err := p.parseVarDefs()
		if err != nil {
			return nil, err
		}
		dirs, err := p.parseDirectives()
		if err != nil {
			return nil, err
		}
		sel, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		return &gqlDefinition{kind: "operation", opType: opType, name: name, varDefs: varDefs, dirs: dirs, selection: sel}, nil
	}
	return nil, &gqlSyntaxError{fmt.Sprintf("Syntax Error: unexpected %s", JSONString(t.value))}
}

func (p *gqlParser) parseVarDefs() ([]gqlVarDef, error) {
	var defs []gqlVarDef
	if !p.acceptPunct("(") {
		return defs, nil
	}
	for !p.acceptPunct(")") {
		if err := p.expectPunct("$"); err != nil {
			return nil, err
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(":"); err != nil {
			return nil, err
		}
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		var def *gqlValue
		if p.acceptPunct("=") {
			v, err := p.parseValue(true)
			if err != nil {
				return nil, err
			}
			def = v
		}
		if _, err := p.parseDirectives(); err != nil {
			return nil, err
		}
		defs = append(defs, gqlVarDef{name: name, typ: typ, def: def})
	}
	return defs, nil
}

func (p *gqlParser) parseType() (*gqlType, error) {
	var typ *gqlType
	if p.acceptPunct("[") {
		inner, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct("]"); err != nil {
			return nil, err
		}
		typ = &gqlType{kind: "list", of: inner}
	} else {
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		typ = &gqlType{kind: "named", name: name}
	}
	if p.acceptPunct("!") {
		typ = &gqlType{kind: "nonNull", of: typ}
	}
	return typ, nil
}

func (p *gqlParser) parseDirectives() ([]gqlDirective, error) {
	var dirs []gqlDirective
	for p.peek().kind == "punct" && p.peek().value == "@" {
		p.next()
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		args, err := p.parseArguments()
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, gqlDirective{name: name, args: args})
	}
	return dirs, nil
}

func (p *gqlParser) parseArguments() (map[string]*gqlValue, error) {
	args := map[string]*gqlValue{}
	if !p.acceptPunct("(") {
		return args, nil
	}
	for !p.acceptPunct(")") {
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(":"); err != nil {
			return nil, err
		}
		v, err := p.parseValue(false)
		if err != nil {
			return nil, err
		}
		args[name] = v
	}
	return args, nil
}

func (p *gqlParser) parseSelectionSet() ([]*gqlSel, error) {
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	var sels []*gqlSel
	for !p.acceptPunct("}") {
		s, err := p.parseSelection()
		if err != nil {
			return nil, err
		}
		sels = append(sels, s)
	}
	return sels, nil
}

func (p *gqlParser) parseSelection() (*gqlSel, error) {
	t := p.peek()
	if t.kind == "punct" && t.value == "..." {
		p.next()
		nt := p.peek()
		if nt.kind == "name" && nt.value != "on" {
			name := p.next().value
			dirs, err := p.parseDirectives()
			if err != nil {
				return nil, err
			}
			return &gqlSel{kind: "spread", fragName: name, dirs: dirs}, nil
		}
		sel := &gqlSel{kind: "inline"}
		if nt.kind == "name" && nt.value == "on" {
			p.next()
			tc, err := p.expectName()
			if err != nil {
				return nil, err
			}
			sel.typeCond = tc
			sel.hasCond = true
		}
		dirs, err := p.parseDirectives()
		if err != nil {
			return nil, err
		}
		sel.dirs = dirs
		inner, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		sel.selection = inner
		return sel, nil
	}
	first, err := p.expectName()
	if err != nil {
		return nil, err
	}
	sel := &gqlSel{kind: "field", name: first}
	if p.acceptPunct(":") {
		sel.alias = first
		sel.hasAlias = true
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		sel.name = name
	}
	args, err := p.parseArguments()
	if err != nil {
		return nil, err
	}
	sel.args = args
	dirs, err := p.parseDirectives()
	if err != nil {
		return nil, err
	}
	sel.dirs = dirs
	if st := p.peek(); st.kind == "punct" && st.value == "{" {
		inner, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		sel.selection = inner
	}
	return sel, nil
}

func (p *gqlParser) parseValue(isConst bool) (*gqlValue, error) {
	t := p.next()
	if t.kind == "punct" && t.value == "$" {
		if isConst {
			return nil, &gqlSyntaxError{"Syntax Error: variable in constant value"}
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		return &gqlValue{kind: "variable", name: name}, nil
	}
	if t.kind == "int" {
		n, err := strconv.ParseInt(t.value, 10, 64)
		if err != nil {
			// JS parseInt saturates to a float; keep the float form.
			f, _ := strconv.ParseFloat(t.value, 64)
			return &gqlValue{kind: "float", floatVal: f}, nil
		}
		return &gqlValue{kind: "int", intVal: n}, nil
	}
	if t.kind == "float" {
		f, _ := strconv.ParseFloat(t.value, 64)
		return &gqlValue{kind: "float", floatVal: f}, nil
	}
	if t.kind == "string" {
		return &gqlValue{kind: "string", strVal: t.value}, nil
	}
	if t.kind == "punct" && t.value == "[" {
		v := &gqlValue{kind: "list"}
		for !(p.peek().kind == "punct" && p.peek().value == "]") {
			item, err := p.parseValue(isConst)
			if err != nil {
				return nil, err
			}
			v.items = append(v.items, item)
		}
		p.next()
		return v, nil
	}
	if t.kind == "punct" && t.value == "{" {
		v := &gqlValue{kind: "object"}
		for !(p.peek().kind == "punct" && p.peek().value == "}") {
			name, err := p.expectName()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct(":"); err != nil {
				return nil, err
			}
			fv, err := p.parseValue(isConst)
			if err != nil {
				return nil, err
			}
			v.fields = append(v.fields, gqlObjField{name: name, value: fv})
		}
		p.next()
		return v, nil
	}
	if t.kind == "name" {
		switch t.value {
		case "true":
			return &gqlValue{kind: "boolean", boolVal: true}, nil
		case "false":
			return &gqlValue{kind: "boolean", boolVal: false}, nil
		case "null":
			return &gqlValue{kind: "null"}, nil
		}
		return &gqlValue{kind: "enum", strVal: t.value}, nil
	}
	return nil, &gqlSyntaxError{fmt.Sprintf("Syntax Error: unexpected value %s", JSONString(t.value))}
}
