package policy

import (
	"strings"
	"unicode/utf8"
)

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokString
	tokInt
	tokFloat
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokComma
	tokDot
	tokEq
	tokNe
	tokLt
	tokLe
	tokGt
	tokGe
	tokAnd
	tokOr
	tokNot
	tokMinus
	tokIn
	tokTrue
	tokFalse
	tokNull
)

// token: pos adalah offset byte di sumber; text berisi pengenal, string yang sudah didekode, atau teks angka.
type token struct {
	kind tokKind
	pos  int
	text string
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

func keywordKind(word string) tokKind {
	switch word {
	case "in":
		return tokIn
	case "true":
		return tokTrue
	case "false":
		return tokFalse
	case "null":
		return tokNull
	default:
		return tokIdent
	}
}

// tokenize memecah sumber menjadi token. Di luar string hanya ASCII yang diterima; karakter lain
// (termasuk "=", "&", "|" tunggal dan tanda kutip tunggal) ditolak, bukan ditafsirkan ulang.
// Token terakhir selalu tokEOF.
func tokenize(src string) ([]token, error) {
	if len(src) > MaxExpressionBytes {
		return nil, tooComplex("panjang ekspresi melebihi %d byte", MaxExpressionBytes)
	}
	if !utf8.ValidString(src) {
		return nil, syntaxErr(0, "bukan UTF-8 yang sah")
	}

	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if len(toks) >= MaxTokens {
			return nil, tooComplex("lebih dari %d token", MaxTokens)
		}
		start := i
		switch {
		case isIdentStart(c):
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			word := src[start:i]
			if len(word) > MaxIdentBytes {
				return nil, tooComplex("pengenal lebih dari %d byte", MaxIdentBytes)
			}
			toks = append(toks, token{kind: keywordKind(word), pos: start, text: word})
		case isDigit(c):
			tok, next, err := lexNumber(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = next
		case c == '"':
			tok, next, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = next
		default:
			tok, width, ok := lexOperator(src, i)
			if !ok {
				return nil, syntaxErr(i, "karakter tidak dikenal")
			}
			toks = append(toks, tok)
			i += width
		}
	}
	toks = append(toks, token{kind: tokEOF, pos: len(src)})
	return toks, nil
}

// lexNumber membaca bilangan bulat atau desimal tanpa eksponen. Titik hanya menjadi bagian angka bila
// diikuti digit; selain itu titik adalah token tersendiri dan parser menolaknya.
func lexNumber(src string, start int) (token, int, error) {
	i := start
	for i < len(src) && isDigit(src[i]) {
		i++
	}
	kind := tokInt
	if i+1 < len(src) && src[i] == '.' && isDigit(src[i+1]) {
		i++
		for i < len(src) && isDigit(src[i]) {
			i++
		}
		kind = tokFloat
	}
	if i < len(src) && isIdentPart(src[i]) {
		return token{}, 0, syntaxErr(start, "angka diikuti huruf atau garis bawah")
	}
	text := src[start:i]
	if len(text) > MaxNumberBytes {
		return token{}, 0, tooComplex("angka lebih dari %d byte", MaxNumberBytes)
	}
	return token{kind: kind, pos: start, text: text}, i, nil
}

// lexString membaca string berkutip ganda. Escape yang dikenal: \" \\ \n \t. Karakter kontrol mentah
// ditolak; isi lain (UTF-8 sah) diteruskan apa adanya.
func lexString(src string, start int) (token, int, error) {
	var b strings.Builder
	i := start + 1
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"':
			return token{kind: tokString, pos: start, text: b.String()}, i + 1, nil
		case c == '\\':
			if i+1 >= len(src) {
				return token{}, 0, syntaxErr(start, "string tidak ditutup")
			}
			switch src[i+1] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return token{}, 0, syntaxErr(i, "escape tidak dikenal")
			}
			i += 2
		case c < 0x20 || c == 0x7f:
			return token{}, 0, syntaxErr(i, "karakter kontrol di dalam string")
		default:
			b.WriteByte(c)
			i++
		}
		if b.Len() > MaxStringBytes {
			return token{}, 0, tooComplex("string lebih dari %d byte", MaxStringBytes)
		}
	}
	return token{}, 0, syntaxErr(start, "string tidak ditutup")
}

// lexOperator membaca operator dan tanda baca. Hasil kedua = jumlah byte yang dipakai.
func lexOperator(src string, i int) (token, int, bool) {
	if i+1 < len(src) {
		if kind, ok := twoCharOperator(src[i : i+2]); ok {
			return token{kind: kind, pos: i}, 2, true
		}
	}
	if kind, ok := oneCharOperator(src[i]); ok {
		return token{kind: kind, pos: i}, 1, true
	}
	return token{}, 0, false
}

func twoCharOperator(s string) (tokKind, bool) {
	switch s {
	case "==":
		return tokEq, true
	case "!=":
		return tokNe, true
	case "<=":
		return tokLe, true
	case ">=":
		return tokGe, true
	case "&&":
		return tokAnd, true
	case "||":
		return tokOr, true
	default:
		return tokEOF, false
	}
}

func oneCharOperator(c byte) (tokKind, bool) {
	switch c {
	case '(':
		return tokLParen, true
	case ')':
		return tokRParen, true
	case '[':
		return tokLBracket, true
	case ']':
		return tokRBracket, true
	case ',':
		return tokComma, true
	case '.':
		return tokDot, true
	case '<':
		return tokLt, true
	case '>':
		return tokGt, true
	case '!':
		return tokNot, true
	case '-':
		return tokMinus, true
	default:
		return tokEOF, false
	}
}
