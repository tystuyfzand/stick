package parse

import (
	"bytes"
	"testing"
)

type lexTest struct {
	name   string
	input  string
	tokens []token
}

func mkTok(t tokenType, val string) token {
	return token{val, t, Pos{0, 0}}
}

var (
	tEOF              = mkTok(TokenEOF, delimEOF)
	tSpace            = mkTok(TokenWhitespace, " ")
	tNewLine          = mkTok(TokenWhitespace, "\n")
	tCommentOpen      = mkTok(TokenCommentOpen, delimOpenComment)
	tCommentClose     = mkTok(TokenCommentClose, delimCloseComment)
	tCommentTrimOpen  = mkTok(TokenCommentOpen, delimOpenComment+delimTrimWhitespace)
	tCommentTrimClose = mkTok(TokenCommentClose, delimTrimWhitespace+delimCloseComment)
	tTagOpen          = mkTok(TokenTagOpen, delimOpenTag)
	tTagClose         = mkTok(TokenTagClose, delimCloseTag)
	tTagTrimOpen      = mkTok(TokenTagOpen, delimOpenTag+delimTrimWhitespace)
	tTagTrimClose     = mkTok(TokenTagClose, delimTrimWhitespace+delimCloseTag)
	tPrintOpen        = mkTok(TokenPrintOpen, delimOpenPrint)
	tPrintClose       = mkTok(TokenPrintClose, delimClosePrint)
	tPrintTrimOpen    = mkTok(TokenPrintOpen, delimOpenPrint+delimTrimWhitespace)
	tPrintTrimClose   = mkTok(TokenPrintClose, delimTrimWhitespace+delimClosePrint)
	tDblStringOpen    = mkTok(TokenStringOpen, "\"")
	tDblStringClose   = mkTok(TokenStringClose, "\"")
	tStringOpen       = mkTok(TokenStringOpen, "'")
	tStringClose      = mkTok(TokenStringClose, "'")
	tInterpolateOpen  = mkTok(TokenInterpolateOpen, delimOpenInterpolate)
	tInterpolateClose = mkTok(TokenInterpolateClose, delimCloseInterpolate)
	tParensOpen       = mkTok(TokenParensOpen, "(")
	tParensClose      = mkTok(TokenParensClose, ")")
)

var lexTests = []lexTest{
	{"empty", "", []token{tEOF}},

	{"comment", "Some text{# Hello there #}", []token{
		mkTok(TokenText, "Some text"),
		tCommentOpen,
		mkTok(TokenText, " Hello there "),
		tCommentClose,
		tEOF,
	}},

	{"unclosed comment", "{# Hello there", []token{
		tCommentOpen,
		mkTok(TokenText, " Hello there"),
		mkTok(TokenError, "expected comment close"),
	}},

	{"number", "{{ 5 }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenNumber, "5"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"operator", "{{\n5 == 4 ? 'Yes' : 'No'\n}}", []token{
		tPrintOpen,
		tNewLine,
		mkTok(TokenNumber, "5"),
		tSpace,
		mkTok(TokenOperator, "=="),
		tSpace,
		mkTok(TokenNumber, "4"),
		tSpace,
		mkTok(TokenPunctuation, "?"),
		tSpace,
		tStringOpen,
		mkTok(TokenText, "Yes"),
		tStringClose,
		tSpace,
		mkTok(TokenPunctuation, ":"),
		tSpace,
		tStringOpen,
		mkTok(TokenText, "No"),
		tStringClose,
		tNewLine,
		tPrintClose,
		tEOF,
	}},

	{"string with operator prefix", "{{ orange }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenName, "orange"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"power and multiply", "{{ 1 ** 10 * 5 }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenNumber, "1"),
		tSpace,
		mkTok(TokenOperator, "**"),
		tSpace,
		mkTok(TokenNumber, "10"),
		tSpace,
		mkTok(TokenOperator, "*"),
		tSpace,
		mkTok(TokenNumber, "5"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"div and floordiv", "{{ 10 // 4 / 2 }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenNumber, "10"),
		tSpace,
		mkTok(TokenOperator, "//"),
		tSpace,
		mkTok(TokenNumber, "4"),
		tSpace,
		mkTok(TokenOperator, "/"),
		tSpace,
		mkTok(TokenNumber, "2"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"is and is not", "{{ 1 is not 10 and 5 is 5 }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenNumber, "1"),
		tSpace,
		mkTok(TokenOperator, "is not"),
		tSpace,
		mkTok(TokenNumber, "10"),
		tSpace,
		mkTok(TokenOperator, "and"),
		tSpace,
		mkTok(TokenNumber, "5"),
		tSpace,
		mkTok(TokenOperator, "is"),
		tSpace,
		mkTok(TokenNumber, "5"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"word operators", "{{ name not in data }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenName, "name"),
		tSpace,
		mkTok(TokenOperator, "not in"),
		tSpace,
		mkTok(TokenName, "data"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"unary not operator", "{{ not 100 }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenOperator, "not"),
		tSpace,
		mkTok(TokenNumber, "100"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"unary negation operator", "{{ -100 }}", []token{
		tPrintOpen,
		tSpace,
		mkTok(TokenOperator, "-"),
		mkTok(TokenNumber, "100"),
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"text", "<html><head></head></html>", []token{
		mkTok(TokenText, "<html><head></head></html>"),
		tEOF,
	}},

	{"simple block", "{% block test %}Some text{% endblock %}", []token{
		tTagOpen,
		tSpace,
		mkTok(TokenName, "block"),
		tSpace,
		mkTok(TokenName, "test"),
		tSpace,
		tTagClose,
		mkTok(TokenText, "Some text"),
		tTagOpen,
		tSpace,
		mkTok(TokenName, "endblock"),
		tSpace,
		tTagClose,
		tEOF,
	}},

	{"print string", "{{ \"this is a test\" }}", []token{
		tPrintOpen,
		tSpace,
		tDblStringOpen,
		mkTok(TokenText, "this is a test"),
		tDblStringClose,
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"unclosed string", "{{ \"this is a test }}", []token{
		tPrintOpen,
		tSpace,
		tDblStringOpen,
		mkTok(TokenError, "unclosed string"),
	}},

	{"unclosed parens", "{{ (test + 5 }}", []token{
		tPrintOpen,
		tSpace,
		tParensOpen,
		mkTok(TokenName, "test"),
		tSpace,
		mkTok(TokenOperator, "+"),
		tSpace,
		mkTok(TokenNumber, "5"),
		tSpace,
		mkTok(TokenError, "unclosed parenthesis"),
	}},

	{"unclosed tag (block)", "{% block test %}", []token{
		tTagOpen,
		tSpace,
		mkTok(TokenName, "block"),
		tSpace,
		mkTok(TokenName, "test"),
		tSpace,
		tTagClose,
		tEOF,
	}},

	{"name with underscore", "{% block additional_javascripts %}", []token{
		tTagOpen,
		tSpace,
		mkTok(TokenName, "block"),
		tSpace,
		mkTok(TokenName, "additional_javascripts"),
		tSpace,
		tTagClose,
		tEOF,
	}},

	{"string interpolation", `{{ "Hello, #{name}" }}`, []token{
		tPrintOpen,
		tSpace,
		tDblStringOpen,
		mkTok(TokenText, "Hello, "),
		tInterpolateOpen,
		mkTok(TokenName, "name"),
		tInterpolateClose,
		tDblStringClose,
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"string interpolation", `{{ "Item #: #{item.id}<br>" }}`, []token{
		tPrintOpen,
		tSpace,
		tDblStringOpen,
		mkTok(TokenText, "Item #: "),
		tInterpolateOpen,
		mkTok(TokenName, "item"),
		mkTok(TokenPunctuation, "."),
		mkTok(TokenName, "id"),
		tInterpolateClose,
		mkTok(TokenText, "<br>"),
		tDblStringClose,
		tSpace,
		tPrintClose,
		tEOF,
	}},

	{"whitespace control print", `{{- test -}}`, []token{
		tPrintTrimOpen,
		tSpace,
		mkTok(TokenName, "test"),
		tSpace,
		tPrintTrimClose,
		tEOF,
	}},

	{"whitespace control tag", `{%- test -%}`, []token{
		tTagTrimOpen,
		tSpace,
		mkTok(TokenName, "test"),
		tSpace,
		tTagTrimClose,
		tEOF,
	}},

	{"whitespace control comment", `{#- test -#}`, []token{
		tCommentTrimOpen,
		mkTok(TokenText, " test "),
		tCommentTrimClose,
		tEOF,
	}},
}

func collect(t *lexTest) (tokens []token) {
	lex := newLexer(bytes.NewReader([]byte(t.input)))
	go lex.tokenize()
	for {
		tok := lex.nextToken()
		tokens = append(tokens, tok)
		if tok.tokenType == TokenEOF || tok.tokenType == TokenError {
			break
		}
	}

	return
}

func equal(stream1, stream2 []token) bool {
	if len(stream1) != len(stream2) {
		return false
	}
	for k := range stream1 {
		switch {
		case stream1[k].tokenType != stream2[k].tokenType,
			stream1[k].value != stream2[k].value:
			return false
		}
	}

	return true
}

func TestLex(t *testing.T) {
	for _, test := range lexTests {
		tokens := collect(&test)
		if !equal(tokens, test.tokens) {
			t.Errorf("%s: got\n\t%+v\nexpected\n\t%v", test.name, tokens, test.tokens)
		}
	}
}
