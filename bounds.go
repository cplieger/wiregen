package wiregen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"math"
	"regexp"
	"strconv"
)

// bounds are the limits a field type declares through its bound methods.
// The binder calls the same methods, so the schema keywords and the
// generated validator checks have one source.
type bounds struct {
	pattern  string
	min, max int64
	maxBytes int
	minItems int
	maxItems int
	hasRange bool
	hasCount bool
}

// boundMethod is one method wiregen reads a bound from: its name and the
// basic kinds of its results.
type boundMethod struct {
	name    string
	results []types.BasicKind
}

var boundMethods = []boundMethod{
	{"Range", []types.BasicKind{types.Int64, types.Int64}},
	{"MaxBytes", []types.BasicKind{types.Int}},
	{"Pattern", []types.BasicKind{types.String}},
	{"Count", []types.BasicKind{types.Int, types.Int}},
}

// namedBounds reads t's bound methods. Each must have a value receiver, its
// boundMethods signature, and a body that is one return of constants, which
// is what lets wiregen read the value without running the code.
func (e *astEngine) namedBounds(t *types.Named) (b *bounds, refusals []string) {
	for _, m := range boundMethods {
		vals, why, ok := e.boundValues(t, m)
		if why != "" {
			refusals = append(refusals, why)
		}
		if !ok {
			continue
		}
		if b == nil {
			b = &bounds{}
		}
		if why := b.set(m.name, vals); why != "" {
			refusals = append(refusals, why)
		}
	}
	return b, refusals
}

// boundValues returns the constants method m of t returns; ok is false when
// t has no such method or it cannot be read, why saying which.
func (e *astEngine) boundValues(t *types.Named, m boundMethod) (vals []constant.Value, why string, ok bool) {
	obj, _, _ := types.LookupFieldOrMethod(t, true, t.Obj().Pkg(), m.name)
	fn, isFunc := obj.(*types.Func)
	if !isFunc {
		return nil, "", false
	}
	if _, ptr := fn.Signature().Recv().Type().(*types.Pointer); ptr {
		return nil, "has a pointer-receiver " + m.name + "; bound methods take a value receiver", false
	}
	if !boundSignature(fn.Signature(), m.results) {
		return nil, m.name + " is not a bound method's signature", false
	}
	vals, err := e.constReturns(fn)
	if err != nil {
		return nil, m.name + " " + err.Error(), false
	}
	return vals, "", true
}

// boundSignature reports whether sig takes nothing and returns exactly the
// basic kinds want.
func boundSignature(sig *types.Signature, want []types.BasicKind) bool {
	if sig.Params().Len() != 0 || sig.Results().Len() != len(want) {
		return false
	}
	for i, k := range want {
		b, ok := sig.Results().At(i).Type().(*types.Basic)
		if !ok || b.Kind() != k {
			return false
		}
	}
	return true
}

// constReturns returns the constant values of fn's body, which must be one
// return statement of constant expressions.
func (e *astEngine) constReturns(fn *types.Func) ([]constant.Value, error) {
	pkg := e.pkgs[fn.Pkg().Path()]
	if pkg == nil || pkg.TypesInfo == nil {
		return nil, fmt.Errorf("is declared in %s, which was not loaded from source", fn.Pkg().Path())
	}
	decl := funcDeclAt(pkg.Syntax, fn)
	if decl == nil || decl.Body == nil || len(decl.Body.List) != 1 {
		return nil, errNotConstReturn
	}
	ret, ok := decl.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != fn.Signature().Results().Len() {
		return nil, errNotConstReturn
	}
	vals := make([]constant.Value, 0, len(ret.Results))
	for _, expr := range ret.Results {
		tv, ok := pkg.TypesInfo.Types[expr]
		if !ok || tv.Value == nil {
			return nil, errNotConstReturn
		}
		vals = append(vals, tv.Value)
	}
	return vals, nil
}

var errNotConstReturn = errors.New("must be one return of constants")

// funcDeclAt returns the declaration of fn in files, or nil.
func funcDeclAt(files []*ast.File, fn *types.Func) *ast.FuncDecl {
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Pos() == fn.Pos() {
				return fd
			}
		}
	}
	return nil
}

// set records method name's values, returning why they are not a bound.
func (b *bounds) set(name string, vals []constant.Value) string {
	if name == "Pattern" {
		b.pattern = constant.StringVal(vals[0])
		if b.pattern == "" {
			return "Pattern is empty"
		}
		if _, err := regexp.Compile(b.pattern); err != nil {
			return "Pattern is not a Go regexp, so no Go binder can check it: " + err.Error()
		}
		return ""
	}
	ints := make([]int64, 0, len(vals))
	for _, v := range vals {
		n, exact := constant.Int64Val(v)
		if !exact {
			return name + " returns a constant outside int64"
		}
		ints = append(ints, n)
	}
	switch name {
	case "Range":
		b.hasRange, b.min, b.max = true, ints[0], ints[1]
		if b.min > b.max {
			return fmt.Sprintf("Range %d to %d is empty", b.min, b.max)
		}
	case "MaxBytes":
		if ints[0] < 1 || ints[0] > math.MaxInt32 {
			return fmt.Sprintf("MaxBytes %d is not a positive byte count", ints[0])
		}
		b.maxBytes = int(ints[0])
	case "Count":
		return b.setCount(ints[0], ints[1])
	}
	return ""
}

// setCount records a Count bound, returning why it is not one.
func (b *bounds) setCount(lo, hi int64) string {
	if lo < 0 || lo > hi || hi > math.MaxInt32 {
		return fmt.Sprintf("Count %d to %d is not 0 <= lo <= hi", lo, hi)
	}
	b.hasCount, b.minItems, b.maxItems = true, int(lo), int(hi)
	return ""
}

// addKeywords writes b's JSON Schema keywords into s.
func (b *bounds) addKeywords(s map[string]any) {
	if b == nil {
		return
	}
	if b.hasRange {
		s["minimum"], s["maximum"] = b.min, b.max
	}
	if b.maxBytes > 0 {
		s["maxLength"], s[maxBytesKeyword] = b.maxBytes, b.maxBytes
	}
	if b.pattern != "" {
		s["pattern"] = b.pattern
	}
	if b.hasCount {
		s["minItems"], s["maxItems"] = b.minItems, b.maxItems
	}
}

// misfit returns why b cannot bound a field of fi's shape, or "": Range bounds
// an integer, MaxBytes and Pattern a string, Count a slice.
func (b *bounds) misfit(fi *fieldInfo) string {
	switch {
	case b.hasRange && fi.Kind != kindInt && fi.Kind != kindUint:
		return "declares Range, which bounds an integer type"
	case (b.maxBytes > 0 || b.pattern != "") && fi.Kind != kindString:
		return "declares MaxBytes or Pattern, which bound a string type"
	case b.hasCount && fi.Kind != kindSlice:
		return "declares Count, which bounds a slice type"
	}
	return ""
}

// validatorCheck returns the TS expression that checks the value expr
// against b at path, or expr unchanged when b is nil.
func (b *bounds) validatorCheck(expr, path string, used *usedIdents) string {
	if b == nil {
		return expr
	}
	if b.hasRange {
		used.helper("checkRange")
		expr = "checkRange(" + expr + ", " + strconv.FormatInt(b.min, 10) + ", " + strconv.FormatInt(b.max, 10) + ", " + path + ")"
	}
	if b.maxBytes > 0 {
		used.helper("checkBytes")
		expr = "checkBytes(" + expr + ", " + strconv.Itoa(b.maxBytes) + ", " + path + ")"
	}
	if b.pattern != "" {
		used.helper("checkPattern")
		expr = "checkPattern(" + expr + ", " + jsString(b.pattern) + ", " + path + ")"
	}
	if b.hasCount {
		used.helper("checkCount")
		expr = "checkCount(" + expr + ", " + strconv.Itoa(b.minItems) + ", " + strconv.Itoa(b.maxItems) + ", " + path + ")"
	}
	return expr
}

// refusesNull reports whether a collection bounded by b may not be null:
// encoding/json decodes null to an empty collection, below a lower bound.
func (b *bounds) refusesNull() bool { return b != nil && b.hasCount && b.minItems >= 1 }
