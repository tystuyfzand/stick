package parse

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"unicode"
)

// TokenType defines a unique type of Token
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenText
	TokenName
	TokenNumber
	TokenCommentOpen
	TokenCommentClose
	TokenTagOpen
	TokenTagClose
	TokenPrintOpen
	TokenPrintClose
	TokenParensOpen
	TokenParensClose
	TokenArrayOpen
	TokenArrayClose
	TokenHashOpen
	TokenHashClose
	TokenStringOpen
	TokenStringClose
	TokenInterpolateOpen
	TokenInterpolateClose
	TokenPunctuation
	TokenOperator
	TokenWhitespace
	TokenError
)

var names = map[TokenType]string{
	TokenText:             "TEXT",
	TokenName:             "NAME",
	TokenNumber:           "NUMBER",
	TokenCommentOpen:      "COMMENT_OPEN",
	TokenCommentClose:     "COMMENT_CLOSE",
	TokenTagOpen:          "TAG_OPEN",
	TokenTagClose:         "TAG_CLOSE",
	TokenPrintOpen:        "PRINT_OPEN",
	TokenPrintClose:       "PRINT_CLOSE",
	TokenParensOpen:       "PARENS_OPEN",
	TokenParensClose:      "PARENS_CLOSE",
	TokenArrayOpen:        "ARRAY_OPEN",
	TokenArrayClose:       "ARRAY_CLOSE",
	TokenHashOpen:         "HASH_OPEN",
	TokenHashClose:        "HASH_CLOSE",
	TokenStringOpen:       "STRING_OPEN",
	TokenStringClose:      "STRING_CLOSE",
	TokenInterpolateOpen:  "INTERPOLATE_OPEN",
	TokenInterpolateClose: "INTERPOLATE_CLOSE",
	TokenPunctuation:      "PUNCTUATION",
	TokenOperator:         "OPERATOR",
	TokenWhitespace:       "WHITESPACE",
	TokenError:            "ERROR",
	TokenEOF:              "EOF",
}

func (typ TokenType) String() string {
	return names[typ]
}

const (
	delimEOF              = ""
	delimOpenTag          = "{%"
	delimCloseTag         = "%}"
	delimOpenPrint        = "{{"
	delimClosePrint       = "}}"
	delimOpenComment      = "{#"
	delimCloseComment     = "#}"
	delimOpenInterpolate  = "#{"
	delimCloseInterpolate = "}"
	delimTrimWhitespace   = "-"
	delimHashKeyValue     = ":"
)

type Token struct {
	value     string
	tokenType TokenType
	Pos
}

func (tok Token) String() string {
	return fmt.Sprintf("{%s '%s' %s}", tok.tokenType, tok.value, tok.Pos)
}

func (tok Token) Value() string {
	return tok.value
}

func (tok Token) Type() TokenType {
	return tok.tokenType
}

// stateFn may emit zero or more tokens.
type stateFn func(*lexer) stateFn

// mode defines the lexer's current operating mode.
type mode int

const (
	modeClosed mode = iota
	modeNormal
	modeInterpolate
)

// lexer contains the current state of a lexer.
type lexer struct {
	start  int // The position of the last emission
	pos    int // The position of the cursor
	line   int // The current line number
	offset int // The current character offset on the current line
	input  string
	tokens chan Token
	state  stateFn
	mode   mode
	last   Token // The last emitted Token
	parens int   // Number of open parenthesis
}

// nextToken returns the next Token emitted by the lexer.
func (l *lexer) nextToken() Token {
	for v, ok := <-l.tokens; ok; {
		l.last = v
		return v
	}

	return l.last
}

// tokenize kicks things off.
func (l *lexer) tokenize() {
	for l.state = lexData; l.state != nil; {
		l.state = l.state(l)
	}
}

// newLexer creates a lexer, ready to begin tokenizing.
func newLexer(input io.Reader) *lexer {
	// TODO: lexer should use the reader.
	i, _ := ioutil.ReadAll(input)
	return &lexer{0, 0, 1, 0, string(i), make(chan Token), nil, modeNormal, Token{}, 0}
}

func (l *lexer) next() (val string) {
	if l.pos >= len(l.input) {
		val = delimEOF

	} else {
		val = l.input[l.pos : l.pos+1]

		l.pos++
	}

	return
}

func (l *lexer) backup() {
	l.pos--
}

func (l *lexer) peek() string {
	val := l.next()
	l.backup()
	return val
}

// emit will create a Token with a value starting from the last emission
// until the current cursor position.
func (l *lexer) emit(t TokenType) {
	val := ""
	if l.pos <= len(l.input) {
		val = l.input[l.start:l.pos]
	}

	tok := Token{val, t, Pos{l.line, l.offset}}

	if c := strings.Count(val, "\n"); c > 0 {
		l.line += c
		lpos := strings.LastIndex(val, "\n")
		l.offset = len(val[lpos+1:])
	} else {
		l.offset += len(val)
	}

	l.tokens <- tok
	l.start = l.pos
	if tok.tokenType == TokenEOF {
		close(l.tokens)
		l.mode = modeClosed
	}
}

func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	tok := Token{fmt.Sprintf(format, args...), TokenError, Pos{l.line, l.offset}}
	l.tokens <- tok

	return nil
}

func lexData(l *lexer) stateFn {
	for {
		switch {
		case strings.HasPrefix(l.input[l.pos:], delimOpenComment):
			if l.pos > l.start {
				l.emit(TokenText)
			}
			return lexCommentOpen

		case strings.HasPrefix(l.input[l.pos:], delimOpenTag):
			if l.pos > l.start {
				l.emit(TokenText)
			}
			return lexTagOpen

		case strings.HasPrefix(l.input[l.pos:], delimOpenPrint):
			if l.pos > l.start {
				l.emit(TokenText)
			}
			return lexPrintOpen
		}

		if l.next() == delimEOF {
			break
		}
	}

	if l.pos > l.start {
		l.emit(TokenText)
	}

	l.emit(TokenEOF)

	return nil
}

func lexExpression(l *lexer) stateFn {
	if l.tryLexOperator() {
		// Special handling for operators is necessary because of the alphabetical
		// operators like "not" and "is".
		return lexExpression
	}
	switch str := l.peek(); {
	case str == delimEOF:
		return lexData

	case strings.HasPrefix(l.input[l.pos:], delimCloseTag),
		strings.HasPrefix(l.input[l.pos:], delimTrimWhitespace+delimCloseTag):
		if l.pos > l.start {
			return l.errorf("pos > start, previous Token not emitted?")
		}
		return lexTagClose

	case strings.HasPrefix(l.input[l.pos:], delimClosePrint),
		strings.HasPrefix(l.input[l.pos:], delimTrimWhitespace+delimClosePrint):
		if l.pos > l.start {
			return l.errorf("pos > start, previous Token not emitted?")
		}
		return lexPrintClose

	case isPunctuation(str):
		return lexPunctuation

	case strings.ContainsAny(str, "([{"):
		return lexOpenParens

	case strings.ContainsAny(str, "}])"):
		return lexCloseParens

	case strings.ContainsAny(str, "\"'"):
		return lexString

	case isNumeric(str):
		return lexNumber

	case isName(str):
		return lexName

	case isSpace(str):
		return lexSpace

	default:
		return l.errorf("unknown expression")
	}
}

// tryLexOperator attempts to match the next sequence of characters to a list of operators.
// This is implemented this way because Twig supports many alphabetical operators like "in",
// which require more than just a check of the next character.
func (l *lexer) tryLexOperator() bool {
	op := operatorMatcher.FindString(l.input[l.pos:])
	if op == "" {
		return false
	} else if op == "%" {
		// Ensure this is not a tag close Token "%}".
		// Go's regexp engine does not support negative lookahead.
		if l.input[l.pos+1:l.pos+2] == "}" {
			return false
		}
	} else if isAlpha(op) {
		// If operator is alphabetic (such as "in" or "is"),
		// we avoid matching "include" or functions like "is_currently_on"
		// For such operators to be valid, they need to have a space after.
		lenOp := len(op)
		if (l.pos+lenOp+1) <= len(l.input) && l.input[l.pos+lenOp:l.pos+lenOp+1] != " " {
			return false
		}
	} else if op == delimTrimWhitespace {
		switch l.input[l.pos+1 : l.pos+3] {
		case delimClosePrint, delimCloseTag:
			return false
		}
	}
	l.pos += len(op)
	l.emit(TokenOperator)

	return true
}

// Check if a string only contains alphabetic characters
func isAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func lexSpace(l *lexer) stateFn {
	for {
		str := l.next()
		if !isSpace(str) {
			l.backup()
			break
		}
	}

	l.emit(TokenWhitespace)

	return lexExpression
}

func lexNumber(l *lexer) stateFn {
	for {
		str := l.next()
		if !isNumeric(str) {
			l.backup()
			break
		}
	}

	l.emit(TokenNumber)

	return lexExpression
}

func lexPunctuation(l *lexer) stateFn {
	for {
		str := l.next()
		if !isPunctuation(str) {
			l.backup()
			break
		}
	}

	l.emit(TokenPunctuation)

	return lexExpression
}

func lexString(l *lexer) stateFn {
	open := l.next()
	l.emit(TokenStringOpen)
	closePos := strings.Index(l.input[l.pos:], open)
	if closePos < 0 {
		return l.errorf("unclosed string")
	}

	if open == `"` && strings.Contains(l.input[l.pos:l.pos+closePos], delimOpenInterpolate) {
		input := l.input
		l.input = input[0 : l.pos+closePos]
		for {
			p := strings.Index(l.input[l.pos:], delimOpenInterpolate)
			if p < 0 {
				break
			}
			l.pos += p
			l.emit(TokenText)
			l.pos += len(delimOpenInterpolate)
			l.emit(TokenInterpolateOpen)
			l.mode = modeInterpolate
			for ins := lexExpression; ins != nil; {
				ins = ins(l)
			}
			if l.mode == modeClosed {
				return nil
			}
			l.mode = modeNormal
			l.emit(TokenInterpolateClose)
		}
		if l.pos < len(l.input) {
			l.pos = len(l.input)
			l.emit(TokenText)
		}
		l.input = input
	} else {
		l.pos += closePos
		l.emit(TokenText)
	}

	l.next()
	l.emit(TokenStringClose)

	return lexExpression
}

func lexOpenParens(l *lexer) stateFn {
	switch str := l.next(); {
	case str == "(":
		l.emit(TokenParensOpen)

	case str == "[":
		l.emit(TokenArrayOpen)

	case str == "{":
		l.emit(TokenHashOpen)

	default:
		return l.errorf("unknown parenthesis")
	}
	l.parens++
	return lexExpression
}

func lexCloseParens(l *lexer) stateFn {
	switch str := l.next(); {
	case str == ")":
		l.emit(TokenParensClose)

	case str == "]":
		l.emit(TokenArrayClose)

	case str == "}":
		if l.parens == 0 && l.mode == modeInterpolate {
			return nil
		}
		l.emit(TokenHashClose)

	default:
		return l.errorf("invalid parenthesis")
	}
	l.parens--
	return lexExpression
}

func lexName(l *lexer) stateFn {
	for {
		str := l.next()
		if str == delimEOF {
			break
		}
		if !isName(str) {
			l.backup()
			break
		}
	}

	l.emit(TokenName)

	return lexExpression
}

func lexCommentOpen(l *lexer) stateFn {
	l.pos += len(delimOpenComment)
	if l.peek() == delimTrimWhitespace {
		l.pos++
	}
	l.emit(TokenCommentOpen)
	til := strings.Index(l.input[l.pos:], delimCloseComment)
	if til < 0 {
		til = len(l.input[l.start:])
	}
	l.pos += til
	if string(l.input[l.pos-1]) == delimTrimWhitespace {
		l.backup()
		l.emit(TokenText)
		l.next()
	} else {
		l.emit(TokenText)
	}
	if !strings.HasPrefix(l.input[l.pos:], delimCloseComment) {
		return l.errorf("expected comment close")
	}
	l.pos += len(delimCloseComment)
	l.emit(TokenCommentClose)

	return lexData
}

func lexTagOpen(l *lexer) stateFn {
	l.pos += len(delimOpenTag)
	if l.peek() == delimTrimWhitespace {
		l.pos++
	}
	l.emit(TokenTagOpen)

	return lexExpression
}

func lexTagClose(l *lexer) stateFn {
	if l.parens > 0 {
		return l.errorf("unclosed parenthesis")
	}
	if l.peek() == delimTrimWhitespace {
		l.pos++
	}
	l.pos += len(delimCloseTag)
	l.emit(TokenTagClose)

	return lexData
}

func lexPrintOpen(l *lexer) stateFn {
	l.pos += len(delimOpenPrint)
	if l.peek() == delimTrimWhitespace {
		l.pos++
	}
	l.emit(TokenPrintOpen)

	return lexExpression
}

func lexPrintClose(l *lexer) stateFn {
	if l.parens > 0 {
		return l.errorf("unclosed parenthesis")
	}
	if l.peek() == delimTrimWhitespace {
		l.pos++
	}
	l.pos += len(delimClosePrint)
	l.emit(TokenPrintClose)

	return lexData
}

func isSpace(str string) bool {
	return str == " " || str == "\t" || str == "\n"
}

func isName(str string) bool {
	for _, s := range str {
		if string(s) != "_" && !unicode.IsLetter(s) && !unicode.IsDigit(s) {
			return false
		}
	}

	return true
}

func isNumeric(str string) bool {
	for _, s := range str {
		if !unicode.IsDigit(s) {
			return false
		}
	}

	return true
}

func isPunctuation(str string) bool {
	for _, s := range str {
		if !strings.ContainsAny(string(s), ",|?:.=") {
			return false
		}
	}

	return true
}
