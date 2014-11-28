package parse

import (
	"bufio"
	"fmt"
	"io"
	"unicode"
)

// Pos is a position in source text.
type Pos struct {
	Name   string
	Offset int
	Line   int
	Col    int
}

func (p *Pos) Copy() *Pos {
	var p2 Pos
	p2 = *p
	return &p2
}

func (p *Pos) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Name, p.Line, p.Col)
}

func (p *Pos) FormatError(tag string, msg string) error {
	return fmt.Errorf("%s error at %s: %s", tag, p, msg)
}

// A token is a single lexeme produced by the scanner.
type token struct {
	typ tokType
	pos *Pos
	val string
}

func (t token) AsError() error {
	if t.typ != tokError {
		panic("AsError called on non-error token")
	}
	return t.pos.FormatError("lex", t.val)
}

type tokType int

const (
	tokEOF tokType = iota

	tokApostrophe   // '
	tokAtSign       // @
	tokBacktick     // `
	tokBool         // true, false
	tokCharLiteral  // \c, \newline, etc
	tokCircumflex   // ^
	tokComment      // ; foobar
	tokDispatch     // any dispatch macro token: #{, #(, #_, etc. Does not include tags.
	tokKeyword      // :foo
	tokLambdaArg    // %, %N
	tokLeftBrace    // {
	tokLeftBracket  // [
	tokLeftParen    // (
	tokNil          // nil
	tokNumber       // any numeric literal; may be invalid (parser will determine)
	tokOctothorpe   // # (only used for tags; dispatch tokens are separate)
	tokRightBrace   // }
	tokRightBracket // ]
	tokRightParen   // )
	tokString       // string literal (java escapes)
	tokSymbol       // foo
	tokTilde        // ~
	// TODO: include whitespace tokens?

	tokError // error; val is the error text
)

var tokTypeToName = map[tokType]string{
	tokApostrophe:   "apostrophe",
	tokAtSign:       "at-sign",
	tokBacktick:     "backtick",
	tokBool:         "bool",
	tokCharLiteral:  "char-literal",
	tokCircumflex:   "circumflex",
	tokComment:      "comment",
	tokDispatch:     "dispatch",
	tokEOF:          "eof",
	tokError:        "error",
	tokKeyword:      "keyword",
	tokLambdaArg:    "lambda-arg",
	tokLeftBrace:    "left-brace",
	tokLeftBracket:  "left-bracket",
	tokLeftParen:    "left-paren",
	tokNil:          "nil",
	tokNumber:       "number",
	tokOctothorpe:   "octothorpe",
	tokRightBrace:   "right-brace",
	tokRightBracket: "right-bracket",
	tokRightParen:   "right-paren",
	tokString:       "string",
	tokSymbol:       "symbol",
	tokTilde:        "tilde",
}

func (t tokType) String() string {
	name, ok := tokTypeToName[t]
	if !ok {
		panic("bad token type")
	}
	return name
}

func (t token) String() string {
	switch t.typ {
	case tokError, tokBool, tokCharLiteral, tokComment, tokKeyword, tokLambdaArg, tokNumber, tokDispatch, tokString, tokSymbol:
		return fmt.Sprintf("<%s@%s>(%q)", t.typ, t.pos, t.val)
	}
	return fmt.Sprintf("<%s@%s>", t.typ, t.pos)
}

// lexer holds the state of the scanner. A single rune of backup is supported.
type lexer struct {
	name    string // the name of the input source
	input   *bufio.Reader
	pos     *Pos // the current position in the input
	start   *Pos // the start position of the token being scanned
	lastPos *Pos // the position before the most recent next() call
	tokens  chan token
	val     []rune // the literal contents of the token
}

func lex(name string, input *bufio.Reader) *lexer {
	l := &lexer{
		name:   name,
		input:  input,
		pos:    &Pos{Name: name, Line: 1, Col: 1},
		start:  &Pos{Name: name, Line: 1, Col: 1},
		tokens: make(chan token),
	}
	go l.run()
	return l
}

type inputReadErr struct {
	err error
}

func (l *lexer) next() (r rune, eof bool) {
	//defer func() {
	//fmt.Printf("next: %q, eof=%t, pos=%s, start=%s, lastPos=%s\n", r, eof, l.pos, l.start, l.lastPos)
	//}()
	r, w, err := l.input.ReadRune()
	if err != nil {
		if err == io.EOF {
			return 0, true
		}
		panic(inputReadErr{err})
	}
	l.lastPos = l.pos.Copy()
	l.pos.Offset += w
	l.pos.Col += w
	if r == '\n' {
		l.pos.Line++
		l.pos.Col = 1
	}
	l.val = append(l.val, r)
	return r, false
}

func (l *lexer) back() {
	if l.lastPos == nil {
		panic("back() call not preceded by a next()")
	}
	if err := l.input.UnreadRune(); err != nil {
		panic("should not happen")
	}
	l.pos = l.lastPos
	l.val = l.val[:len(l.val)-1]
	l.lastPos = nil
}

// scanWhile scans while f(current rune) is true. It does not include the first value for which the predicate
// returns false.
func (l *lexer) scanWhile(f func(r rune) bool) {
	for {
		r, eof := l.next()
		if eof {
			return
		}
		if !f(r) {
			l.back()
			return
		}
	}
}

// scanUntil scans until a rune in set is reached (or EOF). It consumes the discovered element of set, if any.
func (l *lexer) scanUntil(set string) {
	runes := []rune(set)
	for {
		r, eof := l.next()
		if eof {
			return
		}
		for _, r2 := range runes {
			if r == r2 {
				return
			}
		}
	}
}

func (l *lexer) emit(typ tokType) {
	l.tokens <- token{typ, l.start, string(l.val)}
	l.skip()
}

func (l *lexer) skip() {
	l.start = l.pos.Copy()
	l.val = l.val[:0]
}

func (l *lexer) synth(typ tokType, val string) {
	l.tokens <- token{typ, l.start, val}
}

func (l *lexer) nextToken() token {
	return <-l.tokens
}

func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.tokens <- token{tokError, l.start, fmt.Sprintf(format, args...)}
	return nil
}

func (l *lexer) scanError(err error) stateFn {
	l.tokens <- token{tokError, l.start, fmt.Sprintf("error while scanning: %s", err)}
	return nil
}

func (l *lexer) eof() stateFn {
	l.emit(tokEOF)
	return nil
}

// stateFn represents a single state in the scanner.
type stateFn func(*lexer) stateFn

func (l *lexer) run() {
	defer func() {
		if e := recover(); e != nil {
			if e2, ok := e.(inputReadErr); ok {
				l.scanError(e2.err)
				return
			}
			panic(e)
		}
	}()

	for state := lexOuter; state != nil; state = state(l) {
	}
	close(l.tokens)
}

func lexOuter(l *lexer) stateFn {
	r, eof := l.next()
	if eof {
		return l.eof()
	}

	switch r {
	case ';':
		return lexComment
	case '"':
		return lexString
	case '\\':
		return lexCharLiteral
	case ':':
		return lexKeyword
	case '%':
		return lexLambdaArg
	case '#':
		return lexDispatch
	case '+', '-':
		r2, eof := l.next()
		if eof {
			l.emit(tokSymbol)
			return l.eof()
		}
		l.back()
		if r2 >= '0' && r2 <= '9' {
			return lexNumber
		}
		return lexSymbol
	}

	// Recognize single-char tokens
	switch r {
	case '\'':
		l.emit(tokApostrophe)
	case '@':
		l.emit(tokAtSign)
	case '`':
		l.emit(tokBacktick)
	case '^':
		l.emit(tokCircumflex)
	case '{':
		l.emit(tokLeftBrace)
	case '[':
		l.emit(tokLeftBracket)
	case '(':
		l.emit(tokLeftParen)
	case '}':
		l.emit(tokRightBrace)
	case ']':
		l.emit(tokRightBracket)
	case ')':
		l.emit(tokRightParen)
	case '~':
		l.emit(tokTilde)
	default:
		goto afterSingles
	}
	return lexOuter
afterSingles:

	switch {
	case isWhitespace(r):
		return lexWhitespace
	case r >= '0' && r <= '9':
		return lexNumber
	case isSymbolChar(r):
		return lexSymbol
	}
	return l.errorf("unrecognized token starting with %c", r)
}

func lexWhitespace(l *lexer) stateFn {
	l.scanWhile(isWhitespace)
	l.skip()
	return lexOuter
}

func lexComment(l *lexer) stateFn {
	l.scanUntil("\n")
	l.emit(tokComment)
	return lexOuter
}

func lexString(l *lexer) stateFn {
	escaped := false
	for {
		r, eof := l.next()
		if eof {
			return l.errorf("reached EOF before string closing quote")
		}
		switch r {
		case '"':
			if !escaped {
				l.emit(tokString)
				return lexOuter
			}
			escaped = false
		case '\\':
			escaped = !escaped
		default:
			escaped = false
		}
	}
}

func lexCharLiteral(l *lexer) stateFn {
	_, eof := l.next()
	if eof {
		return l.errorf("invalid character literal")
	}
	l.scanWhile(isSymbolChar)
	l.emit(tokCharLiteral)
	return lexOuter
}

func lexKeyword(l *lexer) stateFn {
	l.scanWhile(isSymbolChar)
	l.emit(tokKeyword)
	return lexOuter
}

func lexLambdaArg(l *lexer) stateFn {
	l.scanWhile(isSymbolChar)
	l.emit(tokLambdaArg)
	return lexOuter
}

func lexDispatch(l *lexer) stateFn {
	// Dispatch is tricky. '#foo" and '# foo' are both interpeted as the tag 'foo'. However, '# _' is not
	// interpreted as the ignore macro -- it is the tag '_'. (So the whitespace matters when tokenizing a
	// dispatch macro.) We'll work around this by cheating slightly: if it's a tag, we'll emit an octothorpe
	// token and move on (the subsequent symbol is the tag value). If it's another use of #, the dispatch token
	// we emit will have two chars. The second char will be repeated in the following token. (for instance,
	// "#{1}" will be tokenized as "#{", "{", "1", "}".
	r, eof := l.next()
	if eof {
		l.emit(tokOctothorpe)
		return nil
	}
	val := string(l.val)
	l.back()
	l.skip()
	switch r {
	case '{', '(', '\'', '"', '_':
		l.synth(tokDispatch, val)
		return lexOuter
	}
	return lexOuter
}

func lexNumber(l *lexer) stateFn {
	// There are many different chars that can appear in a number, but it is a subset of symbol chars. Tokenize
	// this way to match the behavior of the clojure compiler. For example: '(+ 3foo)' produces the invalid
	// number '3foo' rather than parsing the same way as '(+ 3 foo)'.
	l.scanWhile(isSymbolChar)
	l.emit(tokNumber)
	return lexOuter
}

func lexSymbol(l *lexer) stateFn {
	l.scanWhile(isSymbolChar)
	l.emit(tokSymbol)
	return lexOuter
}

func isWhitespace(r rune) bool {
	return unicode.IsSpace(r) || r == ','
}

// Decent approximation for now
func isSymbolChar(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '*', '+', '!', '-', '_', '?', '/', '.', ':', '$', '=', '>', '<', '&':
		return true
	}
	return false
}

//func isNumberChar(r rune) bool {
//// There are many ways to write number literals in Clojure. Read http://clojure.org/reader and
//// http://docs.oracle.com/javase/tutorial/java/nutsandbolts/datatypes.html for more information.
////
//// Notes: the following are supported in Java but not Clojure:
//// - Binary literals (0b101)
//// - f/F/d/D suffixes
//// - Underscores inside numbers

//if r >= '0' && r <= '9' {
//return true
//}
//switch r {
//case '.':
//return true
//case '+', '-': // signs as a prefix or on the exponent
//return true
//case 'e', 'E': // exponent
//return true
//case 'r': // specify radix
//return true
//case 'M', 'N': // suffixes for BigDecimal or BigInt
//return true
//case '/': // ratios
//return true
//case 'x': // hex
//return true
//}
//return false
//}
