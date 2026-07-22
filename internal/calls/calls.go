// Package calls infers what a handler reaches out to: databases, other
// services over HTTP or gRPC, caches, and message queues.
//
// # What this can and cannot know
//
// Specter reads syntax, not types. `store.Find(...)` and `cache.Find(...)` are
// the same shape to the AST, and a `Get` could be a map lookup, a cache read,
// or an HTTP call. There is no type checker here to resolve the receiver, so
// classification rests on two syntactic signals:
//
//   - the import an identifier resolves to, when the call goes through a
//     package name (http.Get, sql.Open) — this is reliable;
//   - the receiver's name, when it does not (db.Query, redis.Get) — this is a
//     convention, and conventions are sometimes wrong.
//
// Everything found this way is therefore reported with a Confidence, and the
// console labels the uncertain ones. A dependency map that quietly presents
// guesses as facts is worse than no map: it gets believed.
//
// Calls that cannot be classified are not reported at all. Listing every method
// call in a handler would bury the few that matter.
package calls

import (
	"go/ast"
	"sort"
	"strings"

	"github.com/user/specter/internal/core"
)

// maxDepth bounds how far a handler is followed into the functions it calls.
// Handlers usually delegate — handler → service → repository — so stopping at
// the handler body would miss almost every real dependency. Three levels
// reaches the repository in that shape without walking the whole program.
const maxDepth = 3

// Index holds the declarations of a scanned package and the file each one came
// from, so a call can be followed into the function it names and that
// function's imports can be resolved.
//
// It is per-scan state rather than a package-level map: two scans running at
// once must not see each other's declarations, and a long-lived process
// serving the console should not accumulate every AST it has ever parsed.
type Index struct {
	funcs map[string]*ast.FuncDecl
	files map[*ast.FuncDecl]*ast.File
}

func NewIndex() *Index {
	return &Index{funcs: map[string]*ast.FuncDecl{}, files: map[*ast.FuncDecl]*ast.File{}}
}

// Collect indexes a file's function declarations. Adapters call it once per
// file during the scan.
func (ix *Index) Collect(file *ast.File) {
	if ix == nil {
		return
	}
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			ix.funcs[fd.Name.Name] = fd
			ix.files[fd] = file
		}
	}
}

// Analyze reports what fd reaches. Results are sorted so a regenerated document
// does not churn.
func Analyze(fd *ast.FuncDecl, ix *Index) []core.Call {
	if fd == nil || fd.Body == nil || ix == nil {
		return nil
	}
	a := &analyzer{ix: ix, seen: map[string]bool{}, found: map[string]core.Call{}}
	a.walk(fd, 0)

	out := make([]core.Call, 0, len(a.found))
	for _, c := range a.found {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Target < out[j].Target
	})
	return out
}

type analyzer struct {
	ix    *Index
	seen  map[string]bool // guards against recursion and diamond paths
	found map[string]core.Call
}

func (a *analyzer) walk(fd *ast.FuncDecl, depth int) {
	if fd == nil || fd.Body == nil || depth > maxDepth {
		return
	}
	if a.seen[fd.Name.Name] {
		return
	}
	a.seen[fd.Name.Name] = true

	imports := a.ix.importsOf(fd)

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if c, ok := classify(call, imports); ok {
			// The first sighting is the most direct one, so an entry already
			// recorded is not overwritten by a deeper, weaker one.
			key := c.Kind + " " + c.Target
			if _, dup := a.found[key]; !dup {
				a.found[key] = c
			}
			return true
		}
		// Not a recognised dependency: follow it, since the dependency may be
		// a level down. Handlers usually delegate rather than query directly.
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			a.walk(a.ix.funcs[fn.Name], depth+1)
		case *ast.SelectorExpr:
			// Following by method name is imprecise — two types can share a
			// name — but missing the entire service layer is the worse error.
			a.walk(a.ix.funcs[fn.Sel.Name], depth+1)
		}
		return true
	})
}

// importsOf maps the name an identifier is used under to its import path,
// which is what makes http.Get distinguishable from a variable named http.
func (ix *Index) importsOf(fd *ast.FuncDecl) map[string]string {
	file := ix.files[fd]
	if file == nil {
		return nil
	}
	out := map[string]string{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = path
	}
	return out
}
