package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/user/specter/internal/core"
)

// parseExpr parses a single Go expression.
func parseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return e
}

// parseBody parses a function body from its statements.
func parseBody(t *testing.T, stmts string) *ast.BlockStmt {
	t.Helper()
	src := "package p\nfunc f() {\n" + stmts + "\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v\nsource:\n%s", err, src)
	}
	return file.Decls[0].(*ast.FuncDecl).Body
}

// ---- StringLit ----

func TestStringLit(t *testing.T) {
	got, ok := StringLit(parseExpr(t, `"hello"`))
	if !ok || got != "hello" {
		t.Errorf("got (%q, %v), want (hello, true)", got, ok)
	}
}

func TestStringLitEscapes(t *testing.T) {
	got, ok := StringLit(parseExpr(t, `"a\tb"`))
	if !ok || got != "a\tb" {
		t.Errorf("got (%q, %v), want the unquoted value", got, ok)
	}
}

func TestStringLitRejectsNonStrings(t *testing.T) {
	for _, src := range []string{`42`, `x`, `f()`, "`raw`" + ``} {
		if _, ok := StringLit(parseExpr(t, src)); ok && src != "`raw`" {
			t.Errorf("StringLit(%s) reported ok for a non-string", src)
		}
	}
}

func TestStringLitRawString(t *testing.T) {
	got, ok := StringLit(parseExpr(t, "`raw`"))
	if !ok || got != "raw" {
		t.Errorf("got (%q, %v), want (raw, true)", got, ok)
	}
}

// A malformed string literal cannot reach here from the parser, but the AST
// can be built by hand, and unquoting it must fail rather than panic.
func TestStringLitUnquotableValue(t *testing.T) {
	lit := &ast.BasicLit{Kind: token.STRING, Value: `"unterminated`}
	if got, ok := StringLit(lit); ok {
		t.Errorf("got (%q, true), want ok=false", got)
	}
}

// ---- DocComment ----

func TestDocComment(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		funcName    string
		wantSummary string
		wantDesc    string
	}{
		{"nil comment", "", "F", "", ""},
		{"single line", "// F does a thing.", "F", "does a thing.", ""},
		{"name not stripped when different", "// Other does a thing.", "F", "Other does a thing.", ""},
		{"multi line", "// F does a thing.\n// More detail here.", "F", "does a thing.", "More detail here."},
		{"several detail lines", "// F summary.\n// one\n// two", "F", "summary.", "one\ntwo"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc *ast.CommentGroup
			if tc.text != "" {
				src := "package p\n" + tc.text + "\nfunc " + tc.funcName + "() {}\n"
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				doc = file.Decls[0].(*ast.FuncDecl).Doc
			}
			summary, desc := DocComment(doc, tc.funcName)
			if summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tc.wantSummary)
			}
			if desc != tc.wantDesc {
				t.Errorf("description = %q, want %q", desc, tc.wantDesc)
			}
		})
	}
}

// A comment group that holds only whitespace yields nothing.
func TestDocCommentBlank(t *testing.T) {
	doc := &ast.CommentGroup{List: []*ast.Comment{{Text: "//"}}}
	if s, d := DocComment(doc, "F"); s != "" || d != "" {
		t.Errorf("got (%q, %q), want empty", s, d)
	}
}

// ---- HandlerName ----

func TestHandlerName(t *testing.T) {
	cases := map[string]string{
		"getUser":      "getUser",
		"handlers.Get": "Get",
		`"literal"`:    "",
		"f()":          "",
	}
	for src, want := range cases {
		if got := HandlerName(parseExpr(t, src)); got != want {
			t.Errorf("HandlerName(%s) = %q, want %q", src, got, want)
		}
	}
}

// ---- TypeName ----

func TestTypeName(t *testing.T) {
	cases := []struct {
		src   string
		name  string
		array bool
	}{
		{"User", "User", false},
		{"*User", "User", false},
		{"[]User", "User", true},
		{"[]*User", "User", true},
		{"*[]User", "User", true},
		{"models.User", "User", false},
		{"*models.User", "User", false},
		{"[]models.User", "User", true},
		{"map[string]User", "", false},
		{"func()", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			got := TypeName(parseExpr(t, tc.src))
			if got.Name != tc.name || got.Array != tc.array {
				t.Errorf("= %+v, want {Name:%q Array:%v}", got, tc.name, tc.array)
			}
		})
	}
}

// ---- LocalTypes ----

func TestLocalTypesFromVarDecl(t *testing.T) {
	types := LocalTypes(parseBody(t, "var u User\nvar a, b Item"))
	if types["u"].Name != "User" {
		t.Errorf("u = %+v, want User", types["u"])
	}
	// One declaration may introduce several names; all of them are bound.
	if types["a"].Name != "Item" || types["b"].Name != "Item" {
		t.Errorf("a = %+v, b = %+v, want both Item", types["a"], types["b"])
	}
}

func TestLocalTypesFromCompositeLit(t *testing.T) {
	types := LocalTypes(parseBody(t, "u := User{}\np := &Product{}\nl := []Item{}"))
	if types["u"].Name != "User" {
		t.Errorf("u = %+v", types["u"])
	}
	if types["p"].Name != "Product" {
		t.Errorf("p = %+v", types["p"])
	}
	if types["l"].Name != "Item" || !types["l"].Array {
		t.Errorf("l = %+v, want an Item array", types["l"])
	}
}

// Assignments whose right-hand side has no discoverable type are skipped
// rather than recorded as empty.
func TestLocalTypesSkipsUnknownAssignments(t *testing.T) {
	types := LocalTypes(parseBody(t, "x := f()\ny, z := g()\nvar untyped = 1"))
	if _, ok := types["x"]; ok {
		t.Errorf("x was recorded from an opaque call: %+v", types["x"])
	}
	if _, ok := types["y"]; ok {
		t.Error("multi-assign was recorded")
	}
}

// Declarations that carry no type expression, and non-var declarations, are
// passed over rather than binding an empty type.
func TestLocalTypesSkipsUntypedAndNonVarDecls(t *testing.T) {
	types := LocalTypes(parseBody(t, "var inferred = 1\nconst c = 2\ntype Local struct{}"))
	if _, ok := types["inferred"]; ok {
		t.Errorf("a var with no type expression was bound: %+v", types["inferred"])
	}
	if _, ok := types["c"]; ok {
		t.Error("a const was bound as a local type")
	}
}

func TestLocalTypesEmptyBody(t *testing.T) {
	if got := LocalTypes(parseBody(t, "")); len(got) != 0 {
		t.Errorf("types = %v, want none", got)
	}
}

// ---- ArgType ----

func TestArgType(t *testing.T) {
	types := map[string]TypeInfo{"u": {Name: "User"}, "list": {Name: "Item", Array: true}}

	cases := []struct {
		src  string
		want TypeInfo
	}{
		{"u", TypeInfo{Name: "User"}},
		{"&u", TypeInfo{Name: "User"}},
		{"list", TypeInfo{Name: "Item", Array: true}},
		{"User{}", TypeInfo{Name: "User"}},
		{"&User{}", TypeInfo{Name: "User"}},
		{"[]Item{}", TypeInfo{Name: "Item", Array: true}},
		{"unknown", TypeInfo{}},
		{"f()", TypeInfo{}},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			if got := ArgType(parseExpr(t, tc.src), types); got != tc.want {
				t.Errorf("= %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---- statusValue / statusOr ----

func TestStatusValue(t *testing.T) {
	cases := map[string]int{
		"201":                       201,
		"http.StatusCreated":        201,
		"http.StatusOK":             200,
		"http.StatusNoContent":      204,
		"http.StatusNotFound":       404,
		"StatusInternalServerError": 500,
		"http.StatusTeapot":         0, // not in the table
		"someVar":                   0,
		`"200"`:                     0,
		"f()":                       0,
	}
	for src, want := range cases {
		if got := statusValue(parseExpr(t, src)); got != want {
			t.Errorf("statusValue(%s) = %d, want %d", src, got, want)
		}
	}
}

func TestStatusOr(t *testing.T) {
	if got := statusOr(0, 200); got != 200 {
		t.Errorf("statusOr(0, 200) = %d, want the default", got)
	}
	if got := statusOr(404, 200); got != 404 {
		t.Errorf("statusOr(404, 200) = %d, want the value", got)
	}
}

// ---- dedupeResponses ----

func TestDedupeResponsesEmpty(t *testing.T) {
	if got := dedupeResponses(nil); got != nil {
		t.Errorf("= %+v, want nil", got)
	}
}

// A bare status followed by a typed body at the same code is one response.
func TestDedupeResponsesPrefersTyped(t *testing.T) {
	got := dedupeResponses([]Response{
		{Status: 200, Type: TypeInfo{}},
		{Status: 200, Type: TypeInfo{Name: "User"}},
	})
	if len(got) != 1 {
		t.Fatalf("= %+v, want one entry", got)
	}
	if got[0].Type.Name != "User" {
		t.Errorf("type = %q, want User", got[0].Type.Name)
	}
}

// An existing typed entry is not replaced by a later bare one.
func TestDedupeResponsesKeepsFirstTyped(t *testing.T) {
	got := dedupeResponses([]Response{
		{Status: 200, Type: TypeInfo{Name: "User"}},
		{Status: 200, Type: TypeInfo{}},
	})
	if len(got) != 1 || got[0].Type.Name != "User" {
		t.Errorf("= %+v, want the typed entry kept", got)
	}
}

// Source order is preserved so the document lists responses predictably.
func TestDedupeResponsesPreservesOrder(t *testing.T) {
	got := dedupeResponses([]Response{
		{Status: 201, Type: TypeInfo{Name: "User"}},
		{Status: 400},
		{Status: 201},
	})
	if len(got) != 2 || got[0].Status != 201 || got[1].Status != 400 {
		t.Errorf("= %+v, want 201 then 400", got)
	}
}

// ---- addParam ----

func TestAddParamSkipsDuplicatesAndDynamicNames(t *testing.T) {
	var list []string
	addParam(&list, parseExpr(t, `c.Query("page")`).(*ast.CallExpr))
	addParam(&list, parseExpr(t, `c.Query("page")`).(*ast.CallExpr)) // duplicate
	addParam(&list, parseExpr(t, `c.Query(name)`).(*ast.CallExpr))   // dynamic
	addParam(&list, parseExpr(t, `c.Query()`).(*ast.CallExpr))       // no args
	addParam(&list, parseExpr(t, `c.Query("size")`).(*ast.CallExpr))

	if len(list) != 2 || list[0] != "page" || list[1] != "size" {
		t.Errorf("list = %v, want [page size]", list)
	}
}

// ---- InspectHandler ----

func TestInspectHandlerGinConventions(t *testing.T) {
	h := InspectHandler(parseBody(t, `
		var req CreateUserReq
		c.ShouldBindJSON(&req)
		page := c.Query("page")
		c.DefaultQuery("size", "10")
		c.GetHeader("X-Token")
		c.JSON(201, User{})
	`))

	if h.Request.Name != "CreateUserReq" {
		t.Errorf("request = %+v, want CreateUserReq", h.Request)
	}
	if h.Response.Name != "User" {
		t.Errorf("response = %+v, want User", h.Response)
	}
	if len(h.Query) != 2 || h.Query[0] != "page" || h.Query[1] != "size" {
		t.Errorf("query = %v, want [page size]", h.Query)
	}
	if len(h.Header) != 1 || h.Header[0] != "X-Token" {
		t.Errorf("header = %v, want [X-Token]", h.Header)
	}
	if len(h.Responses) != 1 || h.Responses[0].Status != 201 {
		t.Errorf("responses = %+v, want a single 201", h.Responses)
	}
}

func TestInspectHandlerNetHTTPConventions(t *testing.T) {
	h := InspectHandler(parseBody(t, `
		var req CreateReq
		json.NewDecoder(r.Body).Decode(&req)
		q := r.URL.Query().Get("page")
		tok := r.Header.Get("X-Token")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(User{})
	`))

	if h.Request.Name != "CreateReq" {
		t.Errorf("request = %+v", h.Request)
	}
	if h.Response.Name != "User" {
		t.Errorf("response = %+v", h.Response)
	}
	if len(h.Query) != 1 || h.Query[0] != "page" {
		t.Errorf("query = %v, want [page]", h.Query)
	}
	if len(h.Header) != 1 || h.Header[0] != "X-Token" {
		t.Errorf("header = %v, want [X-Token]", h.Header)
	}
	// WriteHeader(201) then Encode must collapse into one typed 201.
	if len(h.Responses) != 1 || h.Responses[0].Status != 201 || h.Responses[0].Type.Name != "User" {
		t.Errorf("responses = %+v, want one typed 201", h.Responses)
	}
}

func TestInspectHandlerFormValueAndUnmarshal(t *testing.T) {
	h := InspectHandler(parseBody(t, `
		r.FormValue("q")
		json.Unmarshal(body, &Payload{})
	`))
	if len(h.Query) != 1 || h.Query[0] != "q" {
		t.Errorf("query = %v, want [q]", h.Query)
	}
	if h.Request.Name != "Payload" {
		t.Errorf("request = %+v, want Payload", h.Request)
	}
}

func TestInspectHandlerBareStatus(t *testing.T) {
	h := InspectHandler(parseBody(t, `c.Status(http.StatusNoContent)`))
	if len(h.Responses) != 1 || h.Responses[0].Status != 204 {
		t.Errorf("responses = %+v, want a bare 204", h.Responses)
	}
	if h.Responses[0].Type.Name != "" {
		t.Errorf("type = %q, want empty for a bodiless response", h.Responses[0].Type.Name)
	}
}

func TestInspectHandlerMultipleStatuses(t *testing.T) {
	h := InspectHandler(parseBody(t, `
		c.JSON(200, User{})
		c.JSON(404, ErrorBody{})
	`))
	if len(h.Responses) != 2 {
		t.Fatalf("responses = %+v, want two", h.Responses)
	}
	if h.Responses[0].Status != 200 || h.Responses[1].Status != 404 {
		t.Errorf("statuses = %d, %d; want 200, 404", h.Responses[0].Status, h.Responses[1].Status)
	}
	// Response keeps the first body for callers that only handle one.
	if h.Response.Name != "User" {
		t.Errorf("response = %+v, want the first body", h.Response)
	}
}

func TestInspectHandlerBareJSONDefaultsTo200(t *testing.T) {
	h := InspectHandler(parseBody(t, `render.JSON(payload)`))
	if len(h.Responses) != 1 || h.Responses[0].Status != 200 {
		t.Errorf("responses = %+v, want a single 200", h.Responses)
	}
}

func TestInspectHandlerUnresolvableStatusFallsBackTo200(t *testing.T) {
	h := InspectHandler(parseBody(t, `c.JSON(code, User{})`))
	if len(h.Responses) != 1 || h.Responses[0].Status != 200 {
		t.Errorf("responses = %+v, want 200 when the code is dynamic", h.Responses)
	}
}

func TestInspectHandlerIgnoresUnrelatedCalls(t *testing.T) {
	h := InspectHandler(parseBody(t, `
		log.Println("hi")
		doWork()
		x.Frobnicate("q")
	`))
	if h.Request.Name != "" || h.Response.Name != "" {
		t.Errorf("handler = %+v, want nothing extracted", h)
	}
	if len(h.Query) != 0 || len(h.Header) != 0 || len(h.Responses) != 0 {
		t.Errorf("handler = %+v, want nothing extracted", h)
	}
}

// A bare Get() that is neither Query().Get nor Header.Get is not a parameter.
func TestInspectHandlerUnrelatedGetIgnored(t *testing.T) {
	h := InspectHandler(parseBody(t, `cache.Get("key")`))
	if len(h.Query) != 0 || len(h.Header) != 0 {
		t.Errorf("query = %v, header = %v, want both empty", h.Query, h.Header)
	}
}

// Only the first request body is taken; a handler decoding twice keeps the
// first type.
func TestInspectHandlerFirstRequestWins(t *testing.T) {
	h := InspectHandler(parseBody(t, `
		c.ShouldBindJSON(&First{})
		c.ShouldBindJSON(&Second{})
	`))
	if h.Request.Name != "First" {
		t.Errorf("request = %+v, want First", h.Request)
	}
}

// ---- Apply ----

func TestApply(t *testing.T) {
	h := Handler{
		Request:  TypeInfo{Name: "CreateReq"},
		Response: TypeInfo{Name: "User", Array: true},
		Query:    []string{"page"},
		Header:   []string{"X-Token"},
		Responses: []Response{
			{Status: 201, Type: TypeInfo{Name: "User"}},
			{Status: 400, Type: TypeInfo{Name: "Err", Array: true}},
		},
	}
	var route core.Route
	h.Apply(&route)

	if route.RequestType != "CreateReq" || route.RequestArray {
		t.Errorf("request = %q/%v", route.RequestType, route.RequestArray)
	}
	if route.ResponseType != "User" || !route.ResponseArray {
		t.Errorf("response = %q/%v", route.ResponseType, route.ResponseArray)
	}
	if len(route.QueryParams) != 1 || len(route.HeaderParams) != 1 {
		t.Errorf("params = %v / %v", route.QueryParams, route.HeaderParams)
	}
	if len(route.Responses) != 2 {
		t.Fatalf("responses = %+v, want two", route.Responses)
	}
	if route.Responses[1].Type != "Err" || !route.Responses[1].Array {
		t.Errorf("responses[1] = %+v, want an Err array", route.Responses[1])
	}
}

func TestApplyEmptyHandlerLeavesRouteClean(t *testing.T) {
	var route core.Route
	Handler{}.Apply(&route)
	if route.RequestType != "" || route.ResponseType != "" || len(route.Responses) != 0 {
		t.Errorf("route = %+v, want untouched", route)
	}
}

// ---- exprType ----

func TestExprType(t *testing.T) {
	cases := map[string]string{
		"User{}":   "User",
		"&User{}":  "User",
		"[]User{}": "User",
		"x":        "",
		"-x":       "",
		"f()":      "",
	}
	for src, want := range cases {
		if got := exprType(parseExpr(t, src)); got.Name != want {
			t.Errorf("exprType(%s) = %q, want %q", src, got.Name, want)
		}
	}
}
