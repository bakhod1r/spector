package astutil

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"

	"github.com/bakhod1r/spector/internal/core"
)

// StringConsts indexes package-level string const and var declarations by name.
// Only entries whose initializer statically resolves to a string (a string
// literal, another indexed name, or a + concatenation of those) are included.
// It iterates to a fixed point so declaration order and forward references do
// not matter.
func StringConsts(files []*ast.File) map[string]string {
	// Collect candidate name→initializer expressions from top-level const/var.
	type cand struct {
		name string
		expr ast.Expr
	}
	var cands []cand
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue // skip iota / typed-without-value / multi-assign shapes
				}
				for i, name := range vs.Names {
					cands = append(cands, cand{name: name.Name, expr: vs.Values[i]})
				}
			}
		}
	}
	out := map[string]string{}
	// Fixed point: each pass resolves any candidate whose deps are already known.
	for {
		progress := false
		for _, c := range cands {
			if _, done := out[c.name]; done {
				continue
			}
			if v, ok := resolve(c.expr, out); ok {
				out[c.name] = v
				progress = true
			}
		}
		if !progress {
			return out
		}
	}
}

// ResolveString statically evaluates expr to a string using consts:
//   - string literal            → its unquoted value
//   - identifier                → consts[name] if present
//   - X + Y (token.ADD)         → ResolveString(X) + ResolveString(Y)
//   - (inner)                   → ResolveString(inner)
//
// Anything else, or an identifier not in consts, returns ("", false).
// ResolveString(expr, nil) is a behaviour-preserving drop-in for StringLit.
func ResolveString(expr ast.Expr, consts map[string]string) (string, bool) {
	return resolve(expr, consts)
}

func resolve(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	case *ast.ParenExpr:
		return resolve(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, lok := resolve(e.X, consts)
		if !lok {
			return "", false
		}
		r, rok := resolve(e.Y, consts)
		if !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// Diagnostic records a route site the scanner could not statically resolve.
// It is an alias for core.Diagnostic so the core.Adapter interface can
// reference the type without core importing astutil (which imports core).
type Diagnostic = core.Diagnostic

// Diagnostics is an append-only collector; List returns them in source order.
type Diagnostics struct {
	items []Diagnostic
}

func (d *Diagnostics) Add(pos token.Position, kind, reason string) {
	d.items = append(d.items, Diagnostic{Pos: pos, Kind: kind, Reason: reason})
}

func (d *Diagnostics) List() []Diagnostic {
	out := append([]Diagnostic(nil), d.items...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pos.Filename != out[j].Pos.Filename {
			return out[i].Pos.Filename < out[j].Pos.Filename
		}
		return out[i].Pos.Line < out[j].Pos.Line
	})
	return out
}

// DescribeExpr names why an expression is not a static string, for diagnostics.
func DescribeExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return "slice/map element"
	case *ast.CallExpr:
		return "function call result"
	case *ast.SelectorExpr:
		if _, ok := e.X.(*ast.Ident); ok {
			return "identifier from another package"
		}
		return "field or method value"
	case *ast.Ident:
		return "variable value" // a local, or a name not in the const table
	}
	return "non-literal expression"
}
