package parse

import (
	"bytes"
	"errors"
)

// A TagParser can parse the body of a tag, returning the resulting Node or an error.
// TODO: This will be used to implement user-defined tags.
type TagParser func(t *Tree, start Pos) (Node, error)

// parseTag parses the opening of a tag "{%", then delegates to a more specific parser function
// based on the tag's name.
func (t *Tree) parseTag() (Node, error) {
	name, err := t.Expect(TokenName)
	if err != nil {
		return nil, err
	}
	switch name.value {
	case "extends":
		return parseExtends(t, name.Pos)
	case "block":
		return parseBlock(t, name.Pos)
	case "if", "elseif":
		return parseIf(t, name.Pos)
	case "for":
		return parseFor(t, name.Pos)
	case "include":
		return parseInclude(t, name.Pos)
	case "embed":
		return parseEmbed(t, name.Pos)
	case "use":
		return parseUse(t, name.Pos)
	case "set":
		return parseSet(t, name.Pos)
	case "do":
		return parseDo(t, name.Pos)
	case "filter":
		return parseFilter(t, name.Pos)
	case "macro":
		return parseMacro(t, name.Pos)
	case "import":
		return parseImport(t, name.Pos)
	case "from":
		return parseFrom(t, name.Pos)
	case "verbatim":
		return parseVerbatim(t, name.Pos)
	default:
		// Support user-defined parsers
		if p, ok := t.Parsers[name.value]; ok {
			return p(t, name.Pos)
		}

		return nil, newUnexpectedTokenError(name)
	}
}

// ParseUntilEndTag parses until it reaches the specified tag's "end", returning a specific error otherwise.
func (t *Tree) ParseUntilEndTag(name string, start Pos) (*BodyNode, error) {
	tok := t.Peek()
	if tok.tokenType == TokenEOF {
		return nil, newUnclosedTagError(name, start)
	}

	n, err := t.ParseUntilTag(start, "end"+name)
	if err != nil {
		return nil, err
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// ParseUntilTag parses until it reaches the specified tag node, returning a parse error otherwise.
func (t *Tree) ParseUntilTag(start Pos, names ...string) (*BodyNode, error) {
	n := NewBodyNode(start)
	for {
		switch tok := t.Peek(); tok.tokenType {
		case TokenEOF:
			return n, newUnexpectedEOFError(tok)

		case TokenTagOpen:
			t.Next()
			tok, err := t.Expect(TokenName)
			if err != nil {
				return n, err
			}
			if contains(names, tok.value) {
				return n, nil
			}
			t.backup3()
			o, err := t.parse()
			if err != nil {
				return n, err
			}
			n.Append(o)

		default:
			o, err := t.parse()
			if err != nil {
				return n, err
			}
			n.Append(o)
		}
	}
}

// parseExtends parses an extends tag.
//
//	{% extends <expr> %}
func parseExtends(t *Tree, start Pos) (Node, error) {
	if t.Root().Parent != nil {
		return nil, newMultipleExtendsError(start)
	}
	tplRef, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	n := NewExtendsNode(tplRef, start)
	t.Root().Parent = n
	return n, nil
}

// parseBlock parses a block and any body it may contain.
// TODO: {% endblock <name> %} support
//
//	{% block <name> %}
//	{% endblock %}
func parseBlock(t *Tree, start Pos) (Node, error) {
	blockName, err := t.Expect(TokenName)
	if err != nil {
		return nil, err
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	body, err := t.ParseUntilEndTag("block", start)
	if err != nil {
		return nil, err
	}
	nod := NewBlockNode(blockName.value, body, start)
	nod.Origin = t.Name
	t.setBlock(blockName.value, nod)
	return nod, nil
}

// parseIf parses the opening tag and conditional expression in an if-statement.
//
//	{% if <expr> %}
//	{% elseif <expr> %}
func parseIf(t *Tree, start Pos) (Node, error) {
	cond, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	body, els, err := parseIfBody(t, start)
	if err != nil {
		return nil, err
	}
	return NewIfNode(cond, body, els, start), nil
}

// parseIfBody parses the body of an if statement.
//
//	{% else %}
//	{% endif %}
func parseIfBody(t *Tree, start Pos) (body *BodyNode, els *BodyNode, err error) {
	body = NewBodyNode(start)
	for {
		switch tok := t.Peek(); tok.tokenType {
		case TokenEOF:
			return nil, nil, newUnclosedTagError("if", start)
		case TokenTagOpen:
			t.Next()
			tok, err := t.Expect(TokenName)
			if err != nil {
				return nil, nil, err
			}
			switch tok.value {
			case "else":
				_, err := t.Expect(TokenTagClose)
				if err != nil {
					return nil, nil, err
				}
				els, err = t.ParseUntilEndTag("if", start)
				if err != nil {
					return nil, nil, err
				}
			case "elseif":
				t.backup()
				in, err := t.parseTag()
				if err != nil {
					return nil, nil, err
				}
				els = NewBodyNode(tok.Pos, in)
			case "endif":
				_, err := t.Expect(TokenTagClose)
				if err != nil {
					return nil, nil, err
				}
			default:
				// Some other tag nested inside the if
				t.backup()
				n, err := t.parseTag()
				if err != nil {
					return nil, nil, err
				}
				body.Nodes = append(body.Nodes, n)
				continue
			}
			if els == nil {
				els = NewBodyNode(start)
			}
			return body, els, nil
		default:
			n, err := t.parse()
			if err != nil {
				return nil, nil, err
			}
			body.Append(n)
		}
	}
}

// parseFor parses a for loop construct.
// TODO: This needs proper error reporting.
//
//	{% for <name, [name]> in <expr> %}
//	{% for <name, [name]> in <expr> if <expr> %}
//	{% else %}
//	{% endfor %}
func parseFor(t *Tree, start Pos) (*ForNode, error) {
	var kn, vn string
	nam, err := t.parseInnerExpr()
	if err != nil {
		return nil, err
	}
	if nam, ok := nam.(*NameExpr); ok {
		vn = nam.Name
	} else {
		return nil, errors.New("parse error: a parse error occured, expected name")
	}
	nxt := t.PeekNonSpace()
	if nxt.tokenType == TokenPunctuation && nxt.value == "," {
		t.Next()
		kn = vn
		nam, err = t.parseInnerExpr()
		if err != nil {
			return nil, err
		}
		if nam, ok := nam.(*NameExpr); ok {
			vn = nam.Name
		} else {
			return nil, errors.New("parse error: a parse error occured, expected name")
		}
	}
	tok := t.NextNonSpace()
	if tok.tokenType != TokenName && tok.value != "in" {
		return nil, newUnexpectedTokenError(tok)
	}
	expr, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	tok, err = t.Expect(TokenTagClose, TokenName)
	if err != nil {
		return nil, err
	}
	var ifCond Expr
	if tok.tokenType == TokenName {
		if tok.value != "if" {
			return nil, errors.New("parse error: a parse error occured")
		}
		ifCond, err = t.ParseExpr()
		if err != nil {
			return nil, err
		}
		tok, err = t.Expect(TokenTagClose)
		if err != nil {
			return nil, err
		}
	}
	var body Node
	body, err = t.ParseUntilTag(tok.Pos, "endfor", "else")
	if err != nil {
		return nil, err
	}
	if ifCond != nil {
		body = NewIfNode(ifCond, body, nil, tok.Pos)
	}
	t.backup()
	tok = t.Next()
	var elseBody Node = NewBodyNode(tok.Pos)
	if tok.value == "else" {
		_, err = t.Expect(TokenTagClose)
		if err != nil {
			return nil, err
		}
		elseBody, err = t.ParseUntilTag(tok.Pos, "endfor")
		if err != nil {
			return nil, err
		}
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	return NewForNode(kn, vn, expr, body, elseBody, start), nil
}

// parseInclude parses an include statement.
func parseInclude(t *Tree, start Pos) (Node, error) {
	expr, with, only, err := parseIncludeOrEmbed(t)
	if err != nil {
		return nil, err
	}
	return NewIncludeNode(expr, with, only, start), nil
}

// parseEmbed parses an embed statement and body.
func parseEmbed(t *Tree, start Pos) (Node, error) {
	expr, with, only, err := parseIncludeOrEmbed(t)
	if err != nil {
		return nil, err
	}
	t.pushBlockStack()
	for {
		tok := t.NextNonSpace()
		if tok.tokenType == TokenEOF {
			return nil, newUnclosedTagError("embed", start)
		} else if tok.tokenType == TokenTagOpen {
			tok, err := t.Expect(TokenName)
			if err != nil {
				return nil, err
			}
			if tok.value == "endembed" {
				t.Next()
				_, err := t.Expect(TokenTagClose)
				if err != nil {
					return nil, err
				}
				break
			} else if tok.value == "block" {
				n, err := parseBlock(t, start)
				if err != nil {
					return nil, err
				}
				if _, ok := n.(*BlockNode); !ok {
					return nil, newUnexpectedTokenError(tok)
				}
			} else {
				return nil, newUnexpectedValueError(tok, "endembed or block")
			}
		}
	}
	blockRefs := t.popBlockStack()
	return NewEmbedNode(expr, with, only, blockRefs, start), nil
}

// parseIncludeOrEmbed parses an include or embed tag's parameters.
// TODO: Implement "ignore missing" support
//
//	{% include <expr> %}
//	{% include <expr> with <expr> %}
//	{% include <expr> with <expr> only %}
//	{% include <expr> only %}
func parseIncludeOrEmbed(t *Tree) (expr Expr, with Expr, only bool, err error) {
	expr, err = t.ParseExpr()
	if err != nil {
		return
	}
	only = false
	switch tok := t.PeekNonSpace(); tok.tokenType {
	case TokenEOF:
		err = newUnexpectedEOFError(tok)
		return
	case TokenName:
		if tok.value == "only" { // {% include <expr> only %}
			t.Next()
			_, err = t.Expect(TokenTagClose)
			if err != nil {
				return
			}
			only = true
			return expr, with, only, nil
		} else if tok.value != "with" {
			err = newUnexpectedTokenError(tok)
			return
		}
		t.Next()
		with, err = t.ParseExpr()
		if err != nil {
			return
		}
	case TokenTagClose:
	// no op
	default:
		err = newUnexpectedTokenError(tok)
		return
	}
	switch tok := t.NextNonSpace(); tok.tokenType {
	case TokenEOF:
		err = newUnexpectedEOFError(tok)
		return
	case TokenName:
		if tok.value != "only" {
			err = newUnexpectedTokenError(tok)
			return
		}
		_, err = t.Expect(TokenTagClose)
		if err != nil {
			return
		}
		only = true
	case TokenTagClose:
	// no op
	default:
		err = newUnexpectedTokenError(tok)
		return
	}
	return
}

func parseUse(t *Tree, start Pos) (Node, error) {
	tmpl, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	tok, err := t.Expect(TokenName, TokenTagClose)
	if err != nil {
		return nil, err
	}
	aliases := make(map[string]string)
	if tok.tokenType == TokenName {
		if tok.value != "with" {
			return nil, newUnexpectedValueError(tok, "with")
		}
		for {
			orig, err := t.Expect(TokenName)
			if err != nil {
				return nil, err
			}
			tok, err = t.ExpectValue(TokenName, "as")
			if err != nil {
				return nil, err
			}
			alias, err := t.Expect(TokenName)
			if err != nil {
				return nil, err
			}
			aliases[orig.value] = alias.value
			tok, err = t.Expect(TokenTagClose, TokenPunctuation)
			if err != nil {
				return nil, err
			}
			if tok.tokenType == TokenTagClose {
				break
			} else if tok.value != "," {
				return nil, newUnexpectedValueError(tok, ",")
			}
		}
	}
	return NewUseNode(tmpl, aliases, start), nil
}

// parseSet parses a set statement.
//
//	{% set <var> = <expr> %}
//	{% set <var> %}
//	some value
//	{% endset %}
func parseSet(t *Tree, start Pos) (Node, error) {
	tok, err := t.Expect(TokenName)
	if err != nil {
		return nil, err
	}
	var expr Expr
	switch tok := t.NextNonSpace(); tok.tokenType {
	case TokenPunctuation:
		expr, err = t.ParseExpr()
		if err != nil {
			return nil, err
		}
	case TokenTagClose:
		expr, err = t.ParseUntilTag(tok.Pos, "endset")
		if err != nil {
			return nil, err
		}
	default:
		return nil, newUnexpectedTokenError(tok)
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	return NewSetNode(tok.value, expr, start), nil
}

// parseDo parses a do statement.
//
//	{% do <expr> %}
func parseDo(t *Tree, start Pos) (Node, error) {
	expr, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	return NewDoNode(expr, start), nil
}

// parseFilter parses a filter statement.
//
//	{% filter <name> %}
//
// Multiple filters can be applied to a block:
//
//	{% filter <name>|<name>|<name> %}
func parseFilter(t *Tree, start Pos) (Node, error) {
	var filters []string
	for {
		tok, err := t.Expect(TokenName)
		if err != nil {
			return nil, err
		}
		filters = append(filters, tok.value)
		tok = t.PeekNonSpace()
		switch tok.tokenType {
		case TokenEOF:
			return nil, newUnexpectedEOFError(tok)
		case TokenPunctuation:
			if tok.value != "|" {
				return nil, newUnexpectedValueError(tok, "|")
			}
			t.NextNonSpace()
		case TokenTagClose:
			t.NextNonSpace()
			goto body
		}
	}
body:
	body, err := t.ParseUntilEndTag("filter", start)
	if err != nil {
		return nil, err
	}
	return NewFilterNode(filters, body, start), nil
}

// parseMacro parses a macro definition.
//
//	{% macro <name>([ arg [ , arg]) %}
//	Macro body
//	{% endmacro %}
func parseMacro(t *Tree, start Pos) (Node, error) {
	tok, err := t.Expect(TokenName)
	if err != nil {
		return nil, err
	}
	name := tok.value
	_, err = t.Expect(TokenParensOpen)
	if err != nil {
		return nil, err
	}
	var args []string
	for {
		tok = t.NextNonSpace()
		switch tok.tokenType {
		case TokenEOF:
			return nil, newUnexpectedEOFError(tok)
		case TokenName:
			args = append(args, tok.value)
		case TokenPunctuation:
			if tok.value != "," {
				return nil, newUnexpectedValueError(tok, ",")
			}
		case TokenParensClose:
			_, err := t.Expect(TokenTagClose)
			if err != nil {
				return nil, err
			}
			goto body
		default:
			return nil, newUnexpectedTokenError(tok)
		}
	}
body:
	body, err := t.ParseUntilEndTag("macro", start)
	if err != nil {
		return nil, err
	}
	n := NewMacroNode(name, args, body, start)
	n.Origin = t.Name
	t.macros[name] = n
	return n, nil
}

// parseImport parses an import statement.
//
//	{% import <name> as <alias> %}
func parseImport(t *Tree, start Pos) (Node, error) {
	name, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	_, err = t.ExpectValue(TokenName, "as")
	if err != nil {
		return nil, err
	}
	tok, err := t.Expect(TokenName)
	if err != nil {
		return nil, err
	}
	_, err = t.Expect(TokenTagClose)
	if err != nil {
		return nil, err
	}
	return NewImportNode(name, tok.value, start), nil
}

// parseImport parses an import statement.
//
//	{% from <name> import <name>[ as <alias>[ , <name... ] ] %}
func parseFrom(t *Tree, start Pos) (Node, error) {
	name, err := t.ParseExpr()
	if err != nil {
		return nil, err
	}
	_, err = t.ExpectValue(TokenName, "import")
	if err != nil {
		return nil, err
	}
	imports := make(map[string]string)
	for {
		tok := t.NextNonSpace()
		switch tok.tokenType {
		case TokenEOF:
			return nil, newUnexpectedEOFError(tok)
		case TokenName:
			mal := tok.value
			mna := mal
			tok = t.PeekNonSpace()
			if tok.tokenType == TokenName {
				t.NextNonSpace()
				if tok.value != "as" {
					return nil, newUnexpectedValueError(tok, "as")
				}
				tok, err = t.Expect(TokenName)
				if err != nil {
					return nil, err
				}
				mal = tok.value
			}
			imports[mna] = mal
		case TokenPunctuation:
			if tok.value != "," {
				return nil, newUnexpectedValueError(tok, ",")
			}
		case TokenTagClose:
			return NewFromNode(name, imports, start), nil
		default:
			return nil, newUnexpectedTokenError(tok)
		}
	}
}

// parseVerbatim pulls body content within verbatim tag.
//
//	{% verbatim %} body {% endverbatim %}
func parseVerbatim(t *Tree, start Pos) (Node, error) {
	tagName := "verbatim"

	body := bytes.Buffer{}

	if _, err := t.Expect(TokenTagClose); err != nil {
		return nil, err
	}
	for {
		switch tok := t.Peek(); tok.tokenType {
		case TokenEOF:
			return nil, newUnexpectedEOFError(tok)
		case TokenTagOpen:
			tok := t.Next()
			tok, err := t.Expect(TokenName)
			if err != nil {
				return nil, err
			}
			if tok.value == "end"+tagName {
				if _, err := t.Expect(TokenTagClose); err != nil {
					return nil, err
				}
				return NewTextNode(body.String(), start), nil
			}
		default:
			tok := t.Next()
			body.WriteString(tok.value)
		}
	}
}
