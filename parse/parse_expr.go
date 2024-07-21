package parse

import "fmt"

// ParseExpr parses an expression.
func (t *Tree) ParseExpr() (Expr, error) {
	expr, err := t.parseInnerExpr()
	if err != nil {
		return nil, err
	}

	return t.parseOuterExpr(expr)
}

// parseOuterExpr attempts to parse an expression outside of an inner
// expression.
// An outer expression is defined as a modification to an inner expression.
// Examples include attribute accessing, filter application, or binary operations.
func (t *Tree) parseOuterExpr(expr Expr) (Expr, error) {
	switch nt := t.NextNonSpace(); nt.tokenType {
	case TokenParensOpen:
		switch name := expr.(type) {
		case *NameExpr:
			// TODO: This duplicates some code in parseInnerExpr, are both necessary?
			return t.parseFunc(name)
		default:
			return nil, newUnexpectedTokenError(nt)
		}

	case TokenArrayOpen, TokenPunctuation:
		switch nt.value {
		case ".", "[": // Dot or array access
			var args = make([]Expr, 0)
			attr, err := t.parseInnerExpr()
			if err != nil {
				return nil, err
			}

			if nt.value == "[" {
				ntt := t.PeekNonSpace()
				if ntt.tokenType != TokenArrayClose {
					if attr, err = t.parseOuterExpr(attr); err != nil {
						return nil, err
					}
				}
				if _, err := t.Expect(TokenArrayClose); err != nil {
					return nil, err
				}
			} else {
				switch exp := attr.(type) {
				case *NameExpr:
					// valid, but we want to treat the name as a string
					attr = NewStringExpr(exp.Name, exp.Pos)
				case *NumberExpr:
					// Compatibility with Twig: {{ val.0 }}
					attr = NewStringExpr(exp.Value, exp.Pos)
				case *FuncExpr:
					// method call
					for _, v := range exp.Args {
						args = append(args, v)
					}
					attr = NewStringExpr(exp.Name, exp.Pos)
				default:
					return nil, newUnexpectedTokenError(nt)
				}
			}
			return t.parseOuterExpr(NewGetAttrExpr(expr, attr, args, nt.Pos))

		case "|": // Filter application
			nx, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}
			switch n := nx.(type) {
			case *BinaryExpr:
				switch b := n.Left.(type) {
				case *NameExpr:
					v := NewFilterExpr(b.Name, []Expr{expr}, nt.Pos)
					n.Left = v
					return n, nil
				case *FuncExpr:
					b.Args = append([]Expr{expr}, b.Args...)
					v := NewFilterExpr(b.Name, b.Args, nt.Pos)
					n.Left = v
					return n, nil
				default:
					return nil, newUnexpectedTokenError(nt)
				}
			case *NameExpr:
				return NewFilterExpr(n.Name, []Expr{expr}, nt.Pos), nil

			case *FuncExpr:
				n.Args = append([]Expr{expr}, n.Args...)
				return NewFilterExpr(n.Name, n.Args, n.Pos), nil

			default:
				return nil, newUnexpectedTokenError(nt)
			}

		case "?": // Ternary if
			tx, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}
			_, err = t.ExpectValue(TokenPunctuation, ":")
			if err != nil {
				return nil, err
			}
			fx, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}
			return NewTernaryIfExpr(expr, tx, fx, expr.Start()), nil

		default:
			t.backup()
			return expr, nil
		}

	case TokenOperator:
		op, ok := binaryOperators[nt.value]
		if !ok {
			return nil, newUnexpectedTokenError(nt)
		}

		var right Node
		var err error
		if op.op == OpBinaryIs || op.op == OpBinaryIsNot {
			right, err = t.parseRightTestOperand(nil)
			if err != nil {
				return nil, err
			}
		} else {
			right, err = t.ParseExpr()
			if err != nil {
				return nil, err
			}
			if v, ok := right.(*BinaryExpr); ok {
				nxop := binaryOperators[v.Op]
				if nxop.precedence < op.precedence || (nxop.precedence == op.precedence && op.leftAssoc()) {
					left := v.Left
					res := NewBinaryExpr(expr, op.Operator(), left, expr.Start())
					v.Left = res
					return v, nil
				}
			}
		}
		return NewBinaryExpr(expr, op.Operator(), right, expr.Start()), nil

	default:
		t.backup()
		return expr, nil
	}
}

// parseIsRightOperand handles "is" and "is not" tests, which can
// themselves be two words, such as "divisible by":
//
//	{% if 10 is divisible by(3) %}
func (t *Tree) parseRightTestOperand(prev *NameExpr) (*TestExpr, error) {
	right, err := t.parseInnerExpr()
	if err != nil {
		return nil, err
	}
	if prev == nil {
		if r, ok := right.(*NameExpr); ok {
			if nxt := t.PeekNonSpace(); nxt.tokenType == TokenName {
				return t.parseRightTestOperand(r)
			}
		}
	}
	switch r := right.(type) {
	case *NameExpr:
		if prev != nil {
			r.Name = prev.Name + " " + r.Name
		}
		return NewTestExpr(r.Name, []Expr{}, r.Pos), nil

	case *FuncExpr:
		if prev != nil {
			r.Name = prev.Name + " " + r.Name
		}
		return &TestExpr{r}, nil
	default:
		return nil, fmt.Errorf(`Expected name or function, got "%v"`, right)
	}
}

// parseInnerExpr attempts to parse an inner expression.
// An inner expression is defined as a cohesive expression, such as a literal.
func (t *Tree) parseInnerExpr() (Expr, error) {
	switch tok := t.NextNonSpace(); tok.tokenType {
	case TokenEOF:
		return nil, newUnexpectedEOFError(tok)

	case TokenOperator:
		op, ok := unaryOperators[tok.value]
		if !ok {
			return nil, newUnexpectedTokenError(tok)
		}
		expr, err := t.ParseExpr()
		if err != nil {
			return nil, err
		}
		return NewUnaryExpr(op.Operator(), expr, tok.Pos), nil

	case TokenParensOpen:
		inner, err := t.ParseExpr()
		if err != nil {
			return nil, err
		}
		_, err = t.Expect(TokenParensClose)
		if err != nil {
			return nil, err
		}
		return NewGroupExpr(inner, tok.Pos), nil

	case TokenHashOpen:
		els := []*KeyValueExpr{}
		for {
			nxt := t.Peek()
			if nxt.tokenType == TokenHashClose {
				t.Next()
				break
			}
			keyExpr, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}
			_, err = t.ExpectValue(TokenPunctuation, delimHashKeyValue)
			if err != nil {
				return nil, err
			}
			valExpr, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}
			els = append(els, NewKeyValueExpr(keyExpr, valExpr, nxt.Pos))
			nxt = t.Peek()
			if nxt.tokenType == TokenPunctuation {
				_, err := t.ExpectValue(TokenPunctuation, ",")
				if err != nil {
					return nil, err
				}
			}
		}
		return NewHashExpr(tok.Pos, els...), nil

	case TokenArrayOpen:
		els := []Expr{}
		for {
			nxt := t.Peek()
			if nxt.tokenType == TokenArrayClose {
				t.Next()
				break
			}
			expr, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}
			els = append(els, expr)
			nxt = t.Peek()
			if nxt.tokenType == TokenPunctuation {
				_, err := t.ExpectValue(TokenPunctuation, ",")
				if err != nil {
					return nil, err
				}
			}
		}
		return NewArrayExpr(tok.Pos, els...), nil

	case TokenNumber:
		nxt := t.Peek()
		val := tok.value
		if nxt.tokenType == TokenPunctuation && nxt.value == "." {
			val = val + "."
			t.Next()
			nxt, err := t.Expect(TokenNumber)
			if err != nil {
				return nil, err
			}
			val = val + nxt.value
		}
		return NewNumberExpr(val, tok.Pos), nil

	case TokenName:
		switch tok.value {
		case "null", "NULL", "none", "NONE":
			return NewNullExpr(tok.Pos), nil
		case "true", "TRUE":
			return NewBoolExpr(true, tok.Pos), nil
		case "false", "FALSE":
			return NewBoolExpr(false, tok.Pos), nil
		}
		name := NewNameExpr(tok.value, tok.Pos)
		nt := t.NextNonSpace()
		if nt.tokenType == TokenParensOpen {
			// TODO: This duplicates some code in parseOuterExpr, are both necessary?
			return t.parseFunc(name)
		}
		t.backup()
		return name, nil

	case TokenStringOpen:
		var exprs []Expr
		for {
			nxt, err := t.Expect(TokenText, TokenInterpolateOpen, TokenStringClose)
			if err != nil {
				return nil, err
			}
			switch nxt.tokenType {
			case TokenText:
				exprs = append(exprs, NewStringExpr(nxt.value, nxt.Pos))
			case TokenInterpolateOpen:
				exp, err := t.ParseExpr()
				if err != nil {
					return nil, err
				}
				_, err = t.Expect(TokenInterpolateClose)
				if err != nil {
					return nil, err
				}
				exprs = append(exprs, exp)
			case TokenStringClose:
				ln := len(exprs)
				if ln > 1 {
					var res *BinaryExpr
					for i := 1; i < ln; i++ {
						if res == nil {
							res = NewBinaryExpr(exprs[i-1], OpBinaryConcat, exprs[i], exprs[i-1].Start())
							continue
						}
						res = NewBinaryExpr(res, OpBinaryConcat, exprs[i], res.Pos)
					}
					return res, nil
				}
				return exprs[0], nil
			}
		}

	default:
		return nil, newUnexpectedTokenError(tok)
	}
}

// parseFunc parses a function call expression from the first argument expression until the closing parenthesis.
func (t *Tree) parseFunc(name *NameExpr) (Expr, error) {
	var args []Expr
	for {
		switch tok := t.Peek(); tok.tokenType {
		case TokenEOF:
			return nil, newUnexpectedEOFError(tok)

		case TokenParensClose:
		// do nothing

		default:
			argexp, err := t.ParseExpr()
			if err != nil {
				return nil, err
			}

			args = append(args, argexp)
		}

		switch tok := t.NextNonSpace(); tok.tokenType {
		case TokenEOF:
			return nil, newUnexpectedEOFError(tok)

		case TokenPunctuation:
			if tok.value != "," {
				return nil, newUnexpectedValueError(tok, ",")
			}

		case TokenParensClose:
			return NewFuncExpr(name.Name, args, name.Pos), nil

		default:
			return nil, newUnexpectedTokenError(tok, TokenPunctuation, TokenParensClose)
		}
	}
}
