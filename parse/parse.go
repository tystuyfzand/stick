// Package parse handles transforming Stick source code
// into AST for further processing.
package parse // import "github.com/tystuyfzand/stick/parse"

import (
	"bytes"
	"io"
)

// A NodeVisitor can be used to modify node contents and structure.
type NodeVisitor interface {
	Enter(Node) // Enter is called before the node is traversed.
	Leave(Node) // Exit is called before leaving the given Node.
}

// Tree represents the state of a parser.
type Tree struct {
	lex *lexer

	root   *ModuleNode
	blocks []map[string]*BlockNode // Contains each block available to this template.
	macros map[string]*MacroNode   // All macros defined on this template.

	unread []Token // Any tokens received by the lexer but not yet read.
	read   []Token // Tokens that have already been read.

	Name string // A name identifying this tree; the template name.

	Visitors []NodeVisitor
	Parsers  map[string]TagParser
}

// NewTree creates a new parser Tree, ready for use.
func NewTree(input io.Reader) *Tree {
	return NewNamedTree("", input)
}

// NewNamedTree is an alternative constructor which creates a Tree with a name
func NewNamedTree(name string, input io.Reader) *Tree {
	return &Tree{
		lex: newLexer(input),

		root:   NewModuleNode(name),
		blocks: []map[string]*BlockNode{make(map[string]*BlockNode)},
		macros: make(map[string]*MacroNode),

		unread: make([]Token, 0),
		read:   make([]Token, 0),

		Name:     name,
		Visitors: make([]NodeVisitor, 0),
	}
}

// Root returns the root module node.
func (t *Tree) Root() *ModuleNode {
	return t.root
}

// Blocks returns a map of blocks in this tree.
func (t *Tree) Blocks() map[string]*BlockNode {
	return t.blocks[len(t.blocks)-1]
}

// Macros returns a map of macros defined in this tree.
func (t *Tree) Macros() map[string]*MacroNode {
	return t.macros
}

func (t *Tree) popBlockStack() map[string]*BlockNode {
	blocks := t.Blocks()
	t.blocks = t.blocks[0 : len(t.blocks)-1]
	return blocks
}

func (t *Tree) pushBlockStack() {
	t.blocks = append(t.blocks, make(map[string]*BlockNode))
}

func (t *Tree) setBlock(name string, body *BlockNode) {
	t.blocks[len(t.blocks)-1][name] = body
}

func (t *Tree) enrichError(err error) error {
	if err, ok := err.(ParsingError); ok {
		err.setTree(t)
	}
	return err
}

// Peek returns the Next unread Token without advancing the internal cursor.
func (t *Tree) Peek() Token {
	tok := t.Next()
	t.backup()

	return tok
}

// PeekNonSpace returns the Next unread, non-space Token without advancing the internal cursor.
func (t *Tree) PeekNonSpace() Token {
	var next Token
	for {
		next = t.Next()
		if next.tokenType != TokenWhitespace {
			t.backup()
			return next
		}
	}
}

// backup pushes the last read Token back onto the unread stack and reduces the internal cursor by one.
func (t *Tree) backup() {
	var tok Token
	tok, t.read = t.read[len(t.read)-1], t.read[:len(t.read)-1]
	t.unread = append(t.unread, tok)
}

func (t *Tree) backup2() {
	t.backup()
	t.backup()
}

func (t *Tree) backup3() {
	t.backup()
	t.backup()
	t.backup()
}

// Next returns the Next unread Token and advances the internal cursor by one.
func (t *Tree) Next() Token {
	var tok Token
	if len(t.unread) > 0 {
		tok, t.unread = t.unread[len(t.unread)-1], t.unread[:len(t.unread)-1]
	} else {
		tok = t.lex.nextToken()
	}

	t.read = append(t.read, tok)

	return tok
}

// NextNonSpace returns the Next non-whitespace Token.
func (t *Tree) NextNonSpace() Token {
	var next Token
	for {
		next = t.Next()
		if next.tokenType != TokenWhitespace {
			return next
		}
	}
}

// Expect returns the Next non-space Token. Additionally, if the Token is not of one of the expected types,
// an UnexpectedTokenError is returned.
func (t *Tree) Expect(typs ...TokenType) (Token, error) {
	tok := t.NextNonSpace()
	for _, typ := range typs {
		if tok.tokenType == typ {
			return tok, nil
		}
	}

	return tok, newUnexpectedTokenError(tok, typs...)
}

// ExpectValue returns the Next non-space Token, with additional checks on the value of the Token.
// If the Token is not of the expected type, an UnexpectedTokenError is returned. If the Token is not the
// expected value, an UnexpectedValueError is returned.
func (t *Tree) ExpectValue(typ TokenType, val string) (Token, error) {
	tok, err := t.Expect(typ)
	if err != nil {
		return tok, err
	}

	if tok.value != val {
		return tok, newUnexpectedValueError(tok, val)
	}

	return tok, nil
}

// Enter is called when the given Node is entered.
func (t *Tree) enter(n Node) {
	for _, v := range t.Visitors {
		v.Enter(n)
	}
}

// Leave is called just before the state exits the given Node.
func (t *Tree) leave(n Node) {
	for _, v := range t.Visitors {
		v.Leave(n)
	}
}

func (t *Tree) traverse(n Node) {
	if n == nil {
		return
	}
	t.enter(n)
	for _, c := range n.All() {
		t.traverse(c)
	}
	t.leave(n)
}

// Parse parses the given input.
func Parse(input string) (*Tree, error) {
	t := NewTree(bytes.NewReader([]byte(input)))
	return t, t.Parse()
}

// Parse begins parsing, returning an error, if any.
func (t *Tree) Parse() error {
	go t.lex.tokenize()
	for {
		n, err := t.parse()
		if err != nil {
			return t.enrichError(err)
		}
		if n == nil {
			break
		}
		t.root.Append(n)
	}
	t.traverse(t.root)
	return nil
}

// parse parses generic input, such as text markup, print or tag statement opening tokens.
// parse is intended to pick up at the beginning of input, such as the start of a tag's body
// or the more obvious start of a document.
func (t *Tree) parse() (Node, error) {
	tok := t.NextNonSpace()
	switch tok.tokenType {
	case TokenText:
		return NewTextNode(tok.value, tok.Pos), nil

	case TokenPrintOpen:
		name, err := t.ParseExpr()
		if err != nil {
			return nil, err
		}
		_, err = t.Expect(TokenPrintClose)
		if err != nil {
			return nil, err
		}
		return NewPrintNode(name, tok.Pos), nil

	case TokenTagOpen:
		return t.parseTag()

	case TokenCommentOpen:
		tok, err := t.Expect(TokenText)
		if err != nil {
			return nil, err
		}
		_, err = t.Expect(TokenCommentClose)
		if err != nil {
			return nil, err
		}
		return NewCommentNode(tok.value, tok.Pos), nil

	case TokenEOF:
		// expected end of input
		return nil, nil
	}
	return nil, newUnexpectedTokenError(tok)
}
