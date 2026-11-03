package policy

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Env menyediakan nilai untuk akar.atribut. Galat yang dikembalikan Lookup diteruskan apa adanya;
// gunakan ErrNoResource bila sumber daya tidak dimuat dan ErrAttributeUnavailable bila atribut tidak ada.
type Env interface {
	Lookup(root, key string) (any, error)
}

// MapEnv adalah Env sederhana: MapEnv[akar][atribut]. Akar "resource" yang tidak ada berarti
// sumber daya tidak dimuat (ErrNoResource); akar lain yang tidak ada adalah ErrAttributeUnavailable.
type MapEnv map[string]map[string]any

// Lookup mengimplementasikan Env.
func (m MapEnv) Lookup(root, key string) (any, error) {
	attrs, ok := m[root]
	if !ok {
		if root == RootResource {
			return nil, ErrNoResource
		}
		return nil, errAttr(root, key)
	}
	v, ok := attrs[key]
	if !ok {
		return nil, errAttr(root, key)
	}
	return v, nil
}

// errAttr melaporkan nama yang dirujuk (bukan nilainya); errors.Is(err, ErrAttributeUnavailable) berlaku.
func errAttr(root, key string) error {
	return fmt.Errorf("%w: %s.%s", ErrAttributeUnavailable, root, key)
}

// Eval menjalankan program terhadap env. true berarti ekspresi terpenuhi. Setiap kegagalan adalah
// galat (ErrEvaluation, ErrTooComplex, ErrNoResource, ErrAttributeUnavailable, atau galat Env) dan
// pemanggil WAJIB memperlakukannya sebagai penolakan atau penundaan sesuai ADR-0022 butir 31–32;
// nilai false yang menyertai galat tidak boleh dipakai.
func (p *Program) Eval(env Env) (bool, error) {
	if p == nil || p.root == nil {
		return false, evalErr("program kosong")
	}
	if env == nil {
		return false, evalErr("env nil")
	}
	e := &evaluator{env: env}
	v, err := e.eval(p.root)
	if err != nil {
		return false, err
	}
	if v.kind != kindBool {
		return false, evalErr("hasil ekspresi bukan boolean (%s)", v.kind)
	}
	return v.b, nil
}

type evaluator struct {
	env   Env
	steps int
}

func (e *evaluator) step() error {
	e.steps++
	if e.steps > MaxSteps {
		return tooComplex("lebih dari %d langkah evaluasi", MaxSteps)
	}
	return nil
}

func (e *evaluator) eval(n *node) (value, error) {
	if err := e.step(); err != nil {
		return value{}, err
	}
	switch n.kind {
	case nodeLit:
		return n.val, nil
	case nodePath:
		raw, err := e.env.Lookup(n.root, n.key)
		if err != nil {
			return value{}, err
		}
		return normalize(raw)
	case nodeList:
		return e.evalList(n)
	case nodeNot:
		x, err := e.evalBool(n.kids[0])
		if err != nil {
			return value{}, err
		}
		return boolValue(!x), nil
	case nodeBinary:
		return e.evalBinary(n)
	case nodeCall:
		return e.evalCall(n)
	default:
		return value{}, evalErr("simpul tidak dikenal")
	}
}

func (e *evaluator) evalBool(n *node) (bool, error) {
	v, err := e.eval(n)
	if err != nil {
		return false, err
	}
	if v.kind != kindBool {
		return false, evalErr("operand bukan boolean (%s)", v.kind)
	}
	return v.b, nil
}

func (e *evaluator) evalList(n *node) (value, error) {
	out := make([]value, 0, len(n.kids))
	for _, k := range n.kids {
		v, err := e.eval(k)
		if err != nil {
			return value{}, err
		}
		if v.kind == kindList {
			return value{}, evalErr("daftar bersarang tidak didukung")
		}
		out = append(out, v)
	}
	return listValue(out), nil
}

// evalBinary: && dan || memakai hubung-singkat, jadi operand kanan yang tidak perlu dievaluasi tidak
// dapat menimbulkan galat (dan tidak membutuhkan sumber daya). Operator lain mengevaluasi kedua sisi.
func (e *evaluator) evalBinary(n *node) (value, error) {
	if n.op == tokAnd || n.op == tokOr {
		l, err := e.evalBool(n.kids[0])
		if err != nil {
			return value{}, err
		}
		if n.op == tokAnd && !l {
			return boolValue(false), nil
		}
		if n.op == tokOr && l {
			return boolValue(true), nil
		}
		r, err := e.evalBool(n.kids[1])
		if err != nil {
			return value{}, err
		}
		return boolValue(r), nil
	}

	l, err := e.eval(n.kids[0])
	if err != nil {
		return value{}, err
	}
	r, err := e.eval(n.kids[1])
	if err != nil {
		return value{}, err
	}

	switch n.op {
	case tokEq, tokNe:
		eq, err := strictEqual(l, r)
		if err != nil {
			return value{}, err
		}
		return boolValue(eq == (n.op == tokEq)), nil
	case tokLt, tokLe, tokGt, tokGe:
		c, err := compareOrdered(l, r)
		if err != nil {
			return value{}, err
		}
		return boolValue(orderHolds(n.op, c)), nil
	case tokIn:
		return e.evalIn(l, r)
	default:
		return value{}, evalErr("operator tidak dikenal")
	}
}

func orderHolds(op tokKind, c int) bool {
	switch op {
	case tokLt:
		return c < 0
	case tokLe:
		return c <= 0
	case tokGt:
		return c > 0
	default:
		return c >= 0
	}
}

// evalIn: sisi kanan harus daftar; seluruh elemen diperiksa tipenya (tanpa keluar dini) sehingga
// hasil tidak bergantung pada urutan elemen.
func (e *evaluator) evalIn(l, r value) (value, error) {
	if r.kind != kindList {
		return value{}, evalErr("sisi kanan in harus daftar (%s)", r.kind)
	}
	if l.kind == kindList {
		return value{}, evalErr("sisi kiri in tidak boleh daftar")
	}
	found := false
	for _, el := range r.list {
		if err := e.step(); err != nil {
			return value{}, err
		}
		eq, err := strictEqual(l, el)
		if err != nil {
			return value{}, err
		}
		if eq {
			found = true
		}
	}
	return boolValue(found), nil
}

// strictEqual: null hanya sama dengan null (tanpa galat); angka int dan float dibandingkan secara
// numerik; tipe lain yang berbeda adalah galat, bukan false, supaya salah tipe tidak diam-diam
// meloloskan atau melewatkan policy.
func strictEqual(l, r value) (bool, error) {
	if l.kind == kindNull || r.kind == kindNull {
		return l.kind == r.kind, nil
	}
	if l.kind == kindList || r.kind == kindList {
		return false, evalErr("daftar tidak dapat dibandingkan dengan == atau !=")
	}
	if l.isNumber() && r.isNumber() {
		return compareNumbers(l, r) == 0, nil
	}
	if l.kind != r.kind {
		return false, evalErr("tipe %s tidak sebanding dengan %s", l.kind, r.kind)
	}
	if l.kind == kindBool {
		return l.b == r.b, nil
	}
	return l.s == r.s, nil
}

func compareOrdered(l, r value) (int, error) {
	if l.isNumber() && r.isNumber() {
		return compareNumbers(l, r), nil
	}
	if l.kind == kindString && r.kind == kindString {
		return strings.Compare(l.s, r.s), nil
	}
	return 0, evalErr("tipe %s tidak dapat diurutkan terhadap %s", l.kind, r.kind)
}

func compareNumbers(l, r value) int {
	if l.kind == kindInt && r.kind == kindInt {
		switch {
		case l.i < r.i:
			return -1
		case l.i > r.i:
			return 1
		default:
			return 0
		}
	}
	lf, rf := l.asFloat(), r.asFloat()
	switch {
	case lf < rf:
		return -1
	case lf > rf:
		return 1
	default:
		return 0
	}
}

// funcDef mendeskripsikan fungsi daftar putih. Semuanya murni: tanpa I/O dan tanpa akses ke Env.
type funcDef struct {
	arity      int
	boolResult bool
	call       func(args []value) (value, error)
}

func lookupFunc(name string) (funcDef, bool) {
	switch name {
	case "lower":
		return funcDef{arity: 1, call: fnLower}, true
	case "len":
		return funcDef{arity: 1, call: fnLen}, true
	case "startsWith":
		return funcDef{arity: 2, boolResult: true, call: fnStartsWith}, true
	case "endsWith":
		return funcDef{arity: 2, boolResult: true, call: fnEndsWith}, true
	case "contains":
		return funcDef{arity: 2, boolResult: true, call: fnContains}, true
	default:
		return funcDef{}, false
	}
}

func (e *evaluator) evalCall(n *node) (value, error) {
	fn, ok := lookupFunc(n.name)
	if !ok {
		return value{}, evalErr("fungsi tidak dikenal")
	}
	args := make([]value, 0, len(n.kids))
	for _, k := range n.kids {
		v, err := e.eval(k)
		if err != nil {
			return value{}, err
		}
		args = append(args, v)
	}
	if len(args) != fn.arity {
		return value{}, evalErr("jumlah argumen tidak sesuai")
	}
	return fn.call(args)
}

func twoStrings(name string, args []value) (string, string, error) {
	if args[0].kind != kindString || args[1].kind != kindString {
		return "", "", evalErr("%s membutuhkan dua string (%s, %s)", name, args[0].kind, args[1].kind)
	}
	return args[0].s, args[1].s, nil
}

func fnLower(args []value) (value, error) {
	if args[0].kind != kindString {
		return value{}, evalErr("lower membutuhkan string (%s)", args[0].kind)
	}
	return stringValue(strings.ToLower(args[0].s)), nil
}

func fnLen(args []value) (value, error) {
	switch args[0].kind {
	case kindString:
		return intValue(int64(utf8.RuneCountInString(args[0].s))), nil
	case kindList:
		return intValue(int64(len(args[0].list))), nil
	default:
		return value{}, evalErr("len membutuhkan string atau daftar (%s)", args[0].kind)
	}
}

func fnStartsWith(args []value) (value, error) {
	s, prefix, err := twoStrings("startsWith", args)
	if err != nil {
		return value{}, err
	}
	return boolValue(strings.HasPrefix(s, prefix)), nil
}

func fnEndsWith(args []value) (value, error) {
	s, suffix, err := twoStrings("endsWith", args)
	if err != nil {
		return value{}, err
	}
	return boolValue(strings.HasSuffix(s, suffix)), nil
}

func fnContains(args []value) (value, error) {
	s, sub, err := twoStrings("contains", args)
	if err != nil {
		return value{}, err
	}
	return boolValue(strings.Contains(s, sub)), nil
}
