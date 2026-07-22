// Package lint reports routing mistakes that compile fine and fail silently.
//
// "A dead endpoint" is not quite the right phrase for a REST API: every
// registered route is reachable over HTTP, so none of them is unreachable in
// the sense a dead function is. What does happen, and what this package finds,
// is three failures that produce no compiler error and no runtime error — just
// an endpoint that never runs, or runs when a different one was meant to:
//
//   - a handler that is written but never registered (orphan);
//   - the same method and path registered twice, where the framework silently
//     keeps one and drops the other, or panics at startup;
//   - a literal path shadowed by a parameterised one registered before it, so
//     /users/me is answered by the /users/{id} handler.
//
// Each finding names a file and line, so the output is usable from CI.
package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/core"
)

// Kinds of finding.
const (
	OrphanHandler  = "orphan-handler"
	DuplicateRoute = "duplicate-route"
	ShadowedRoute  = "shadowed-route"
)

// Finding is one problem, with where to look.
type Finding struct {
	Kind    string       `json:"kind"`
	Message string       `json:"message"`
	Source  *core.Source `json:"source,omitempty"`
}

func (f Finding) String() string {
	where := ""
	if f.Source != nil {
		where = fmt.Sprintf("%s:%d: ", f.Source.File, f.Source.Line)
	}
	return where + f.Kind + ": " + f.Message
}

// Analyze reports problems in the routes scanned from dir. Results are sorted
// so a CI run's output is stable.
func Analyze(dir string, routes []core.Route) ([]Finding, error) {
	var out []Finding

	orphans, err := orphanHandlers(dir, routes)
	if err != nil {
		return nil, err
	}
	out = append(out, orphans...)
	out = append(out, duplicates(routes)...)
	out = append(out, shadowed(routes)...)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Message < out[j].Message
	})
	return out, nil
}

// duplicates finds the same method and path registered more than once. gin
// panics on this, chi panics, and net/http's newer mux panics — but only for
// the exact same pattern, and only at startup, which a test that never starts
// the router will not catch.
func duplicates(routes []core.Route) []Finding {
	seen := map[string]core.Route{}
	var out []Finding
	for _, r := range routes {
		key := strings.ToLower(r.Method) + " " + r.Path
		if first, dup := seen[key]; dup {
			// Route.Source points at the handler's declaration, which is the
			// right answer for documentation but means two registrations of
			// the same handler share one position. Citing it twice would read
			// as a bug in the tool, so the other location is only named when
			// it is genuinely a different one.
			msg := fmt.Sprintf("%s %s is registered more than once; one registration will never run",
				strings.ToUpper(r.Method), r.Path)
			switch {
			case first.HandlerName != r.HandlerName:
				msg += fmt.Sprintf(" (%s at %s, and %s at %s)",
					first.HandlerName, at(first.Source), r.HandlerName, at(r.Source))
			case r.HandlerName != "":
				msg += fmt.Sprintf(" (both registered to %s)", r.HandlerName)
			}
			out = append(out, Finding{Kind: DuplicateRoute, Message: msg, Source: r.Source})
			continue
		}
		seen[key] = r
	}
	return out
}

// shadowed finds a literal segment that a parameterised route already covers,
// where registration order decides the winner. /users/{id} registered before
// /users/me means a request for /users/me is answered by the {id} handler in
// routers that match in registration order.
//
// This is reported for every framework, not only the ones that behave this way,
// because the ambiguity is worth knowing about regardless: a reader of the code
// cannot tell which handler serves /users/me without knowing the router's
// matching rules.
func shadowed(routes []core.Route) []Finding {
	var out []Finding
	for i, later := range routes {
		if hasParam(later.Path) {
			continue // a parameterised route is the shadower, not the shadowed
		}
		for j := 0; j < i; j++ {
			earlier := routes[j]
			if !strings.EqualFold(earlier.Method, later.Method) || !hasParam(earlier.Path) {
				continue
			}
			if !covers(earlier.Path, later.Path) {
				continue
			}
			out = append(out, Finding{
				Kind: ShadowedRoute,
				Message: fmt.Sprintf("%s %s may be shadowed by %s registered earlier at %s",
					strings.ToUpper(later.Method), later.Path, earlier.Path, at(earlier.Source)),
				Source: later.Source,
			})
		}
	}
	return out
}

// covers reports whether a parameterised pattern would match a literal path:
// same number of segments, and every segment either equal or a parameter.
func covers(pattern, literal string) bool {
	p, l := split(pattern), split(literal)
	if len(p) != len(l) {
		return false
	}
	sawParam := false
	for i := range p {
		switch {
		case isParam(p[i]):
			sawParam = true
		case p[i] != l[i]:
			return false
		}
	}
	return sawParam
}

func split(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func hasParam(path string) bool {
	for _, s := range split(path) {
		if isParam(s) {
			return true
		}
	}
	return false
}

func at(s *core.Source) string {
	if s == nil {
		return "an unknown location"
	}
	return fmt.Sprintf("%s:%d", s.File, s.Line)
}

// orphanHandlers finds functions shaped like handlers that no route registers.
// These are usually a rename or a deleted registration: the code still
// compiles, the endpoint quietly stops existing, and nothing says so.
func orphanHandlers(dir string, routes []core.Route) ([]Finding, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}

	registered := map[string]bool{}
	for _, r := range routes {
		registered[r.HandlerName] = true
	}

	loc := astutil.Locator{Fset: fset, Dir: dir}
	var out []Finding
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				if registered[fd.Name.Name] || !looksLikeHandler(fd) {
					continue
				}
				out = append(out, Finding{
					Kind: OrphanHandler,
					Message: fmt.Sprintf("%s looks like a handler but no route registers it",
						fd.Name.Name),
					Source: loc.Of(fd.Pos()),
				})
			}
		}
	}
	return out, nil
}

// looksLikeHandler recognises the parameter shapes the supported frameworks
// use. Matching on the signature rather than the name avoids flagging every
// unexported helper in the package.
func looksLikeHandler(fd *ast.FuncDecl) bool {
	if fd.Type.Params == nil {
		return false
	}
	var params []string
	for _, f := range fd.Type.Params.List {
		name := typeString(f.Type)
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			params = append(params, name)
		}
	}
	// The result list matters as much as the parameters. A gin handler returns
	// nothing and an echo handler returns exactly one error; a function taking
	// *gin.Context and returning something else is a helper, and flagging it
	// would make the check noisy enough to be switched off.
	results := 0
	if fd.Type.Results != nil {
		for _, f := range fd.Type.Results.List {
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			results += n
		}
	}

	switch {
	case len(params) == 1 && params[0] == "gin.Context":
		return results == 0
	case len(params) == 1 && params[0] == "echo.Context":
		return results == 1 && typeString(fd.Type.Results.List[0].Type) == "error"
	case len(params) == 2 && params[0] == "http.ResponseWriter" && params[1] == "http.Request":
		return results == 0
	}
	return false
}

// typeString renders a parameter type as pkg.Name, dropping pointers, which is
// enough to recognise the handler signatures without a type checker.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return typeString(t.X)
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}
