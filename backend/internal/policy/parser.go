package policy

import (
	"math"
	"strconv"
)

type nodeKind uint8

const (
	nodeLit nodeKind = iota
	nodePath
	nodeList
	nodeNot
	nodeBinary
	nodeCall
)

// node adalah simpul AST. nodeLit memakai val; nodePath memakai root dan key; nodeList memakai kids
// sebagai elemen; nodeNot memakai kids[0]; nodeBinary memakai op dan kids[0], kids[1]; nodeCall memakai
// name dan kids sebagai argumen.
type node struct {
	kind nodeKind
	op   tokKind
	val  value
	root string
	key  string
	name string
	kids []*node
	pos  int
}

// Program adalah ekspresi yang sudah lolos parse. Tidak dapat diubah setelah dibuat sehingga aman
// dipakai bersamaan oleh banyak goroutine.
type Program struct {
	root *node
}

// Parse mengurai ekspresi policy. Galatnya membungkus ErrSyntax atau ErrTooComplex. Ekspresi yang
// pasti tidak menghasilkan boolean (mis. string atau angka literal) ditolak di sini.
func Parse(expr string) (*Program, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	if len(toks) == 1 {
		return nil, syntaxErr(0, "ekspresi kosong")
	}
	p := &parser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, syntaxErr(t.pos, "token tak terduga setelah ekspresi")
	}
	if !mayBeBool(n) {
		return nil, syntaxErr(n.pos, "ekspresi harus menghasilkan boolean")
	}
	return &Program{root: n}, nil
}

type parser struct {
	toks  []token
	i     int
	depth int
}

func (p *parser) peek() token {
	return p.toks[p.i]
}

// next mengembalikan token saat ini dan maju, kecuali di tokEOF (tidak pernah melewati ujung).
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > MaxDepth {
		return tooComplex("kedalaman bersarang melebihi %d", MaxDepth)
	}
	return nil
}

func (p *parser) leave() {
	p.depth--
}

func isCompareOp(k tokKind) bool {
	switch k {
	case tokEq, tokNe, tokLt, tokLe, tokGt, tokGe, tokIn:
		return true
	default:
		return false
	}
}

func validRoot(name string) bool {
	return name == RootResource || name == RootCurrentUser || name == RootScope
}

func (p *parser) parseOr() (*node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		op := p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &node{kind: nodeBinary, op: tokOr, kids: []*node{left, right}, pos: op.pos}
	}
	return left, nil
}

func (p *parser) parseAnd() (*node, error) {
	left, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokAnd {
		op := p.next()
		right, err := p.parseCompare()
		if err != nil {
			return nil, err
		}
		left = &node{kind: nodeBinary, op: tokAnd, kids: []*node{left, right}, pos: op.pos}
	}
	return left, nil
}

// parseCompare: perbandingan tidak berantai (a == b == c ditolak) supaya maknanya tidak ambigu.
func (p *parser) parseCompare() (*node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if !isCompareOp(p.peek().kind) {
		return left, nil
	}
	op := p.next()
	right, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); isCompareOp(t.kind) {
		return nil, syntaxErr(t.pos, "perbandingan berantai tidak diizinkan; pakai && atau tanda kurung")
	}
	return &node{kind: nodeBinary, op: op.kind, kids: []*node{left, right}, pos: op.pos}, nil
}

func (p *parser) parseUnary() (*node, error) {
	if p.peek().kind != tokNot {
		return p.parsePrimary()
	}
	t := p.next()
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	x, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	return &node{kind: nodeNot, kids: []*node{x}, pos: t.pos}, nil
}

func (p *parser) parsePrimary() (*node, error) {
	t := p.peek()
	switch t.kind {
	case tokTrue:
		p.next()
		return &node{kind: nodeLit, val: boolValue(true), pos: t.pos}, nil
	case tokFalse:
		p.next()
		return &node{kind: nodeLit, val: boolValue(false), pos: t.pos}, nil
	case tokNull:
		p.next()
		return &node{kind: nodeLit, val: value{}, pos: t.pos}, nil
	case tokString:
		p.next()
		return &node{kind: nodeLit, val: stringValue(t.text), pos: t.pos}, nil
	case tokInt, tokFloat:
		return p.parseNumber(false)
	case tokMinus:
		p.next()
		if k := p.peek().kind; k != tokInt && k != tokFloat {
			return nil, syntaxErr(t.pos, "tanda minus hanya untuk angka literal")
		}
		return p.parseNumber(true)
	case tokLParen:
		p.next()
		if err := p.enter(); err != nil {
			return nil, err
		}
		defer p.leave()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if c := p.peek(); c.kind != tokRParen {
			return nil, syntaxErr(c.pos, "kurung tutup diharapkan")
		}
		p.next()
		return n, nil
	case tokLBracket:
		return p.parseList()
	case tokIdent:
		return p.parseIdent()
	default:
		return nil, syntaxErr(t.pos, "operand diharapkan")
	}
}

func (p *parser) parseNumber(neg bool) (*node, error) {
	t := p.next()
	text := t.text
	if neg {
		text = "-" + text
	}
	if t.kind == tokInt {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, syntaxErr(t.pos, "bilangan bulat di luar jangkauan")
		}
		return &node{kind: nodeLit, val: intValue(n), pos: t.pos}, nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, syntaxErr(t.pos, "bilangan desimal tidak sah")
	}
	return &node{kind: nodeLit, val: floatValue(f), pos: t.pos}, nil
}

func (p *parser) parseList() (*node, error) {
	open := p.next()
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	n := &node{kind: nodeList, pos: open.pos}
	if p.peek().kind == tokRBracket {
		p.next()
		return n, nil
	}
	for {
		el, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		n.kids = append(n.kids, el)
		if len(n.kids) > MaxListElements {
			return nil, tooComplex("daftar lebih dari %d elemen", MaxListElements)
		}
		switch t := p.peek(); t.kind {
		case tokComma:
			p.next()
		case tokRBracket:
			p.next()
			return n, nil
		default:
			return nil, syntaxErr(t.pos, "koma atau ] diharapkan")
		}
	}
}

// parseIdent menangani akar.atribut (tepat dua segmen) dan panggilan fungsi. Pengenal yang berdiri
// sendiri ditolak: tidak ada variabel bebas.
func (p *parser) parseIdent() (*node, error) {
	id := p.next()
	switch p.peek().kind {
	case tokDot:
		p.next()
		attr := p.peek()
		if attr.kind != tokIdent {
			return nil, syntaxErr(attr.pos, "nama atribut diharapkan setelah titik")
		}
		p.next()
		if !validRoot(id.text) {
			return nil, syntaxErr(id.pos, "akar %q tidak dikenal; gunakan resource, current_user, atau scope", id.text)
		}
		if t := p.peek(); t.kind == tokDot {
			return nil, syntaxErr(t.pos, "hanya dua segmen (akar.atribut) yang didukung")
		}
		return &node{kind: nodePath, root: id.text, key: attr.text, pos: id.pos}, nil
	case tokLParen:
		return p.parseCall(id)
	default:
		return nil, syntaxErr(id.pos, "pengenal %q berdiri sendiri; gunakan akar.atribut atau panggilan fungsi", id.text)
	}
}

func (p *parser) parseCall(id token) (*node, error) {
	fn, ok := lookupFunc(id.text)
	if !ok {
		return nil, syntaxErr(id.pos, "fungsi %q tidak ada dalam daftar putih", id.text)
	}
	p.next()
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	n := &node{kind: nodeCall, name: id.text, pos: id.pos}
	if p.peek().kind != tokRParen {
		for {
			arg, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			n.kids = append(n.kids, arg)
			if len(n.kids) > fn.arity {
				return nil, syntaxErr(id.pos, "fungsi %q menerima %d argumen", id.text, fn.arity)
			}
			if p.peek().kind != tokComma {
				break
			}
			p.next()
		}
	}
	if c := p.peek(); c.kind != tokRParen {
		return nil, syntaxErr(c.pos, "kurung tutup diharapkan")
	}
	p.next()
	if len(n.kids) != fn.arity {
		return nil, syntaxErr(id.pos, "fungsi %q menerima %d argumen", id.text, fn.arity)
	}
	return n, nil
}

// mayBeBool melaporkan apakah simpul mungkin menghasilkan boolean. Hanya pemeriksaan statis kasar;
// hasil akhir tetap diperiksa saat evaluasi.
func mayBeBool(n *node) bool {
	switch n.kind {
	case nodeLit:
		return n.val.kind == kindBool
	case nodeList:
		return false
	case nodeCall:
		fn, ok := lookupFunc(n.name)
		return ok && fn.boolResult
	default:
		return true
	}
}
