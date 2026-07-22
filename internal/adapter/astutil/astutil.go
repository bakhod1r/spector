package astutil

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/user/specter/internal/core"
)

type TypeInfo struct {
	Name  string
	Array bool
}

func StringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// Directives are optional lines in a handler's doc comment that state what the
// AST cannot infer. Specter's promise is that a project needs no annotations —
// and it keeps that promise: omit these and everything still works. Writing one
// only adds detail that has no other source.
//
//	// ListUsers returns every user.
//	// specter:tags users,admin
//	// specter:operationId listAllUsers
//	func listUsers(c *gin.Context) { ... }
//
// The prefix is namespaced so a directive cannot be confused with prose, and
// directive lines are stripped from the description: they are instructions to
// the generator, not documentation for a reader.
const directivePrefix = "specter:"

// Directives holds what a doc comment declared.
type Directives struct {
	Tags        []string
	OperationID string
	Deprecated  bool
}

// ParseDirectives reads the specter: lines out of a doc comment. Unknown
// directives are ignored rather than reported: a newer Specter may understand
// one this version does not, and failing on it would make the annotation
// riskier to adopt than leaving it out.
func ParseDirectives(doc *ast.CommentGroup) Directives {
	var d Directives
	if doc == nil {
		return d
	}
	for _, line := range strings.Split(doc.Text(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, directivePrefix) {
			continue
		}
		name, arg, _ := strings.Cut(strings.TrimPrefix(line, directivePrefix), " ")
		arg = strings.TrimSpace(arg)
		switch name {
		case "tags":
			for _, t := range strings.Split(arg, ",") {
				if t = strings.TrimSpace(t); t != "" {
					d.Tags = append(d.Tags, t)
				}
			}
		case "operationId":
			d.OperationID = arg
		case "deprecated":
			d.Deprecated = true
		}
	}
	return d
}

// DocComment splits a handler's doc comment into an OpenAPI summary (first
// paragraph line) and description (the remaining lines). A leading "Name "
// prefix (Go's convention of starting a doc comment with the identifier) is
// stripped from the summary. Both are empty when there is no comment.
func DocComment(doc *ast.CommentGroup, funcName string) (summary, description string) {
	if doc == nil {
		return "", ""
	}
	// Directive lines are instructions to the generator, not prose; leaving
	// them in would put "specter:tags users" in the rendered description.
	var kept []string
	for _, line := range strings.Split(doc.Text(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), directivePrefix) {
			continue
		}
		kept = append(kept, line)
	}
	text := strings.TrimSpace(strings.Join(kept, "\n"))
	if text == "" {
		return "", ""
	}
	lines := strings.Split(text, "\n")
	summary = strings.TrimSpace(lines[0])
	summary = strings.TrimPrefix(summary, funcName+" ")
	rest := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	return summary, rest
}

// SourceOf resolves a node's position into a Source relative to the scanned
// directory. It returns nil rather than a half-filled value when the position
// is unknown, so a caller can never mistake a zero line for line 1.
//
// The path is made relative to dir because the document is meant to be
// committed and shared: an absolute path would differ per machine and leak the
// developer's home directory. When the file lies outside dir — possible if a
// handler is defined elsewhere — the original path is kept, since a relative
// path climbing out of the tree would be worse than an honest absolute one.
func SourceOf(fset *token.FileSet, dir string, pos token.Pos) *core.Source {
	if fset == nil || !pos.IsValid() {
		return nil
	}
	p := fset.Position(pos)
	if p.Filename == "" {
		return nil
	}
	file := p.Filename
	if rel, err := filepath.Rel(dir, file); err == nil && !strings.HasPrefix(rel, "..") {
		file = rel
	}
	return &core.Source{File: filepath.ToSlash(file), Line: p.Line}
}

// Locator carries what SourceOf needs so adapters can pass one value down
// through their route-collecting helpers instead of threading a file set and a
// directory through every signature.
type Locator struct {
	Fset *token.FileSet
	Dir  string
}

// Of resolves a position, or returns nil for the zero Locator so a caller that
// has no file set does not have to special-case it.
func (l Locator) Of(pos token.Pos) *core.Source {
	return SourceOf(l.Fset, l.Dir, pos)
}

// Handler picks the best position for a route: the handler's declaration when
// there is one, otherwise the registration call. A route registered with a
// closure or a returned handler has no declaration to point at, and the line
// that registered it is still more useful than nothing.
func (l Locator) Handler(fd *ast.FuncDecl, call ast.Node) *core.Source {
	if fd != nil {
		if s := l.Of(fd.Pos()); s != nil {
			return s
		}
	}
	if call != nil {
		return l.Of(call.Pos())
	}
	return nil
}

func HandlerName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

func TypeName(expr ast.Expr) TypeInfo {
	switch t := expr.(type) {
	case *ast.Ident:
		return TypeInfo{Name: t.Name}
	case *ast.StarExpr:
		return TypeName(t.X)
	case *ast.ArrayType:
		inner := TypeName(t.Elt)
		inner.Array = true
		return inner
	case *ast.SelectorExpr:
		return TypeInfo{Name: t.Sel.Name}
	}
	return TypeInfo{}
}

// Returns indexes the result type of every function and method in a package,
// by name. It is what lets a handler that writes the result of a call —
// `out, err := db.listUsers()` — still be documented, which is how most real
// handlers are written; only examples assign composite literals.
//
// Methods are indexed by their bare name as well as recv.Name, because the
// call site says `db.listUsers()` and knowing the type of `db` would require a
// full type check. Two methods of the same name on different types therefore
// collide, and the last one parsed wins. That is a real limitation: it is
// bounded (it can only affect a body whose handler calls such a method) and it
// is still far better than documenting no response type at all.
func Returns(files []*ast.File) map[string]TypeInfo {
	out := map[string]TypeInfo{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Type.Results == nil {
				continue
			}
			info, ok := firstResult(fd.Type.Results)
			if !ok {
				continue
			}
			out[fd.Name.Name] = info
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				if recv := TypeName(fd.Recv.List[0].Type); recv.Name != "" {
					out[recv.Name+"."+fd.Name.Name] = info
				}
			}
		}
	}
	return out
}

// firstResult picks the result that carries the payload: the first one that is
// not an error. A function returning only an error has nothing to document.
func firstResult(results *ast.FieldList) (TypeInfo, bool) {
	for _, field := range results.List {
		info := TypeName(field.Type)
		if info.Name == "" || info.Name == "error" {
			continue
		}
		return info, true
	}
	return TypeInfo{}, false
}

func LocalTypes(body *ast.BlockStmt) map[string]TypeInfo {
	return LocalTypesWith(body, nil)
}

// LocalTypesWith resolves local variable types, using returns to follow the
// results of function calls when it is supplied.
func LocalTypesWith(body *ast.BlockStmt, returns map[string]TypeInfo) map[string]TypeInfo {
	types := map[string]TypeInfo{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.DeclStmt:
			gd, ok := t.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Type == nil {
					continue
				}
				info := TypeName(vs.Type)
				for _, id := range vs.Names {
					types[id.Name] = info
				}
			}
		case *ast.AssignStmt:
			if len(t.Rhs) != 1 {
				return true
			}
			// A single-value assignment names the type directly. A multi-value
			// one is the `out, err := f()` shape, where the payload is the
			// first non-error result and the rest are not worth naming.
			if len(t.Lhs) == 1 {
				id, ok := t.Lhs[0].(*ast.Ident)
				if !ok {
					return true
				}
				if info := exprType(t.Rhs[0]); info.Name != "" {
					types[id.Name] = info
				} else if info := callType(t.Rhs[0], returns); info.Name != "" {
					types[id.Name] = info
				}
				return true
			}
			info := callType(t.Rhs[0], returns)
			if info.Name == "" {
				return true
			}
			for _, lhs := range t.Lhs {
				id, ok := lhs.(*ast.Ident)
				// "_" names nothing, and "err" holds the error rather than the
				// payload.
				if !ok || id.Name == "_" || id.Name == "err" {
					continue
				}
				types[id.Name] = info
				break
			}
		}
		return true
	})
	return types
}

func ArgType(expr ast.Expr, types map[string]TypeInfo) TypeInfo {
	if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.AND {
		expr = u.X
	}
	if info := exprType(expr); info.Name != "" {
		return info
	}
	if id, ok := expr.(*ast.Ident); ok {
		return types[id.Name]
	}
	return TypeInfo{}
}

// Handler holds everything an adapter needs from a single handler body:
// the request/response Go types plus the query and header parameter names
// the handler reads. It unifies the gin (c.Query, c.GetHeader, c.JSON,
// c.ShouldBindJSON) and net/http (r.URL.Query().Get, r.Header.Get,
// r.FormValue, json.Decoder/Encoder) conventions in one pass so every
// adapter extracts the same information.
type Handler struct {
	Request  TypeInfo
	Response TypeInfo // first response body, kept for backward compatibility
	Query    []string
	Header   []string
	// Responses lists every status-coded response the handler emits, in source
	// order: gin c.JSON(201, x), net/http w.WriteHeader(code) + Encode(x), etc.
	Responses []Response
}

// Response is a single status-coded response discovered in a handler.
type Response struct {
	Status int
	Type   TypeInfo
}

// Apply copies the inspected request/response types, query and header
// parameters, and status-coded responses onto a route. Adapters call it after
// InspectHandler so every framework maps handler facts to the core model the
// same way.
func (h Handler) Apply(route *core.Route) {
	route.RequestType, route.RequestArray = h.Request.Name, h.Request.Array
	route.ResponseType, route.ResponseArray = h.Response.Name, h.Response.Array
	route.QueryParams = h.Query
	route.HeaderParams = h.Header
	for _, r := range h.Responses {
		route.Responses = append(route.Responses, core.RouteResponse{
			Status: r.Status,
			Type:   r.Type.Name,
			Array:  r.Type.Array,
		})
	}
}

// InspectHandler walks a handler body and reports its request/response types,
// the query/header parameters it reads, and every status-coded response it
// emits. It unifies the gin and net/http conventions in one pass.
func InspectHandler(body *ast.BlockStmt) Handler {
	return InspectHandlerWith(body, nil)
}

// InspectHandlerWith is InspectHandler with a package-level index of function
// result types, so handlers that write the result of a call are documented too.
func InspectHandlerWith(body *ast.BlockStmt, returns map[string]TypeInfo) Handler {
	types := LocalTypesWith(body, returns)
	var h Handler
	pending := 0 // net/http status set by a preceding w.WriteHeader(code)
	addResp := func(status int, t TypeInfo) {
		h.Responses = append(h.Responses, Response{Status: status, Type: t})
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Decode", "DecodeJSON", "Unmarshal":
			if h.Request.Name == "" && len(call.Args) >= 1 {
				h.Request = ArgType(call.Args[len(call.Args)-1], types)
			}
		case "ShouldBindJSON", "BindJSON", "ShouldBind", "Bind":
			// "Bind" is echo's spelling of the same thing.
			if h.Request.Name == "" && len(call.Args) == 1 {
				h.Request = ArgType(call.Args[0], types)
			}
		case "Encode":
			// net/http response body; status comes from a preceding WriteHeader.
			if len(call.Args) == 1 {
				t := ArgType(call.Args[0], types)
				if h.Response.Name == "" {
					h.Response = t
				}
				addResp(statusOr(pending, 200), t)
				pending = 0
			}
		case "JSON", "IndentedJSON", "AbortWithStatusJSON", "PureJSON", "XML", "YAML":
			// gin c.JSON(code, body): first arg is the status code.
			if len(call.Args) >= 2 {
				status := statusValue(call.Args[0])
				t := ArgType(call.Args[len(call.Args)-1], types)
				if h.Response.Name == "" {
					h.Response = t
				}
				addResp(statusOr(status, 200), t)
			} else if len(call.Args) == 1 {
				// render.JSON(w, r, x) style handled via Encode; here a bare
				// JSON(x) is treated as a 200 body.
				t := ArgType(call.Args[0], types)
				if h.Response.Name == "" {
					h.Response = t
				}
				addResp(200, t)
			}
		case "Status", "AbortWithStatus", "NoContent":
			// gin c.Status(code) / echo c.NoContent(code): a bodiless response.
			if len(call.Args) == 1 {
				if s := statusValue(call.Args[0]); s != 0 {
					addResp(s, TypeInfo{})
				}
			}
		case "WriteHeader":
			// net/http w.WriteHeader(code): remember for the next Encode; also
			// record it in case the handler writes no JSON body.
			if len(call.Args) == 1 {
				if s := statusValue(call.Args[0]); s != 0 {
					pending = s
					addResp(s, TypeInfo{})
				}
			}
		case "Query", "DefaultQuery", "GetQuery", "QueryArray", "FormValue",
			"QueryParam", "QueryParams":
			// The last two are echo's spelling.
			addParam(&h.Query, call)
		case "GetHeader":
			addParam(&h.Header, call)
		case "Get":
			// net/http: r.URL.Query().Get("q") vs r.Header.Get("X-Token").
			switch x := sel.X.(type) {
			case *ast.CallExpr: // ...Query().Get(...)
				if inner, ok := x.Fun.(*ast.SelectorExpr); ok && inner.Sel.Name == "Query" {
					addParam(&h.Query, call)
				}
			case *ast.SelectorExpr: // r.Header.Get(...)
				if x.Sel.Name == "Header" {
					addParam(&h.Header, call)
				}
			}
		}
		return true
	})
	h.Responses = dedupeResponses(h.Responses)
	h.Response = primaryResponse(h.Responses, h.Response)
	return h
}

// primaryResponse picks the type that best represents "what this endpoint
// returns". Handlers commonly write an error envelope before the success body
// (`if err != nil { c.JSON(400, Err{}) }` … `c.JSON(200, User{})`), so taking
// whatever came first would document the envelope as the response type.
// The first 2xx body wins; anything else falls back to source order.
func primaryResponse(responses []Response, fallback TypeInfo) TypeInfo {
	for _, r := range responses {
		if r.Status >= 200 && r.Status < 300 && r.Type.Name != "" {
			return r.Type
		}
	}
	for _, r := range responses {
		if r.Type.Name != "" {
			return r.Type
		}
	}
	return fallback
}

// dedupeResponses collapses responses that share a status code, preferring the
// entry that carries a body type. A bare WriteHeader/Status followed by an
// Encode at the same code should surface as one typed response.
func dedupeResponses(in []Response) []Response {
	if len(in) == 0 {
		return nil
	}
	order := []int{}
	byStatus := map[int]Response{}
	for _, r := range in {
		prev, seen := byStatus[r.Status]
		if !seen {
			order = append(order, r.Status)
			byStatus[r.Status] = r
			continue
		}
		if prev.Type.Name == "" && r.Type.Name != "" {
			byStatus[r.Status] = r
		}
	}
	out := make([]Response, 0, len(order))
	for _, s := range order {
		out = append(out, byStatus[s])
	}
	return out
}

func statusOr(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// statusValue resolves an HTTP status code expression: an integer literal
// (201) or a net/http constant selector (http.StatusCreated). Returns 0 when
// it can't be resolved.
func statusValue(expr ast.Expr) int {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.INT {
			if n, err := strconv.Atoi(e.Value); err == nil {
				return n
			}
		}
	case *ast.SelectorExpr:
		return httpStatus[e.Sel.Name]
	case *ast.Ident:
		return httpStatus[e.Name]
	}
	return 0
}

// httpStatus maps net/http status constant names to their numeric codes.
var httpStatus = map[string]int{
	"StatusOK":                  200,
	"StatusCreated":             201,
	"StatusAccepted":            202,
	"StatusNoContent":           204,
	"StatusMovedPermanently":    301,
	"StatusFound":               302,
	"StatusNotModified":         304,
	"StatusBadRequest":          400,
	"StatusUnauthorized":        401,
	"StatusForbidden":           403,
	"StatusNotFound":            404,
	"StatusMethodNotAllowed":    405,
	"StatusConflict":            409,
	"StatusGone":                410,
	"StatusUnprocessableEntity": 422,
	"StatusTooManyRequests":     429,
	"StatusInternalServerError": 500,
	"StatusNotImplemented":      501,
	"StatusBadGateway":          502,
	"StatusServiceUnavailable":  503,
}

// addParam appends the first string-literal argument of call to list, skipping
// duplicates and non-literal (dynamic) names.
func addParam(list *[]string, call *ast.CallExpr) {
	if len(call.Args) < 1 {
		return
	}
	name, ok := StringLit(call.Args[0])
	if !ok {
		return
	}
	for _, x := range *list {
		if x == name {
			return
		}
	}
	*list = append(*list, name)
}

// callType resolves the payload type of a call expression from the package
// index. Without an index it resolves nothing, which keeps LocalTypes'
// behaviour unchanged for callers that do not supply one.
func callType(expr ast.Expr, returns map[string]TypeInfo) TypeInfo {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(returns) == 0 {
		return TypeInfo{}
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return returns[fun.Name]
	case *ast.SelectorExpr:
		// db.listUsers() and s.store.listUsers() alike: the method name is the
		// only part that can be matched without a type check.
		if recv, ok := fun.X.(*ast.Ident); ok {
			if info, found := returns[recv.Name+"."+fun.Sel.Name]; found {
				return info
			}
		}
		return returns[fun.Sel.Name]
	}
	return TypeInfo{}
}

func exprType(expr ast.Expr) TypeInfo {
	switch t := expr.(type) {
	case *ast.CompositeLit:
		return TypeName(t.Type)
	case *ast.UnaryExpr:
		if t.Op == token.AND {
			return exprType(t.X)
		}
	}
	return TypeInfo{}
}
