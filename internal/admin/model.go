// Package admin turns an OpenAPI document into an admin panel: a master list
// per resource, a read-only detail view, and the actions the API actually
// supports.
//
// The important word is "actually". A panel that offers Edit on a resource with
// no update endpoint is worse than no panel, because the button exists and does
// nothing. Every control here is derived from an operation that was found in
// the code, so a read-only API generates a read-only screen.
package admin

import (
	"sort"
	"strings"
	"unicode"

	"github.com/user/specter/internal/core"
)

// Resource is one thing an operator manages: a collection path plus whatever
// operations exist on it and on its item path.
type Resource struct {
	// Name is the slug used in admin URLs (/admin/users).
	Name string
	// Label and Singular are what an operator reads. Both come from the
	// handler names in the code where possible, because those are the names
	// the team chose; the path is only a fallback.
	Label    string
	Singular string

	// CollectionPath is /api/v1/users, ItemPath is /api/v1/users/{id}.
	CollectionPath string
	ItemPath       string
	// IDParam is the path parameter naming an item, "id" in the usual case.
	IDParam string

	List   *Action
	Get    *Action
	Create *Action
	Update *Action
	Delete *Action

	// Fields are the columns of the master table, in schema order.
	Fields []Field
	// Form are the inputs a create or update needs.
	Form []Field
}

// Action is one API operation the panel calls.
type Action struct {
	Method  string
	Path    string
	Label   string // from the handler name: "Open cart", not a hardcoded "Create"
	Handler string
	Summary string
	// Headers are the header parameters the operation requires, which on a
	// guarded API is how the panel learns it must forward a signature.
	Headers []string
	// Secured reports whether middleware protects this operation.
	Secured bool
}

// Field is one property of a resource.
type Field struct {
	Name     string
	Label    string
	Type     string // string | number | integer | boolean | array | object
	Format   string
	Required bool
	Enum     []string
	// Primary marks the field that identifies an item, so the master table can
	// make it the clickable cell.
	Primary bool

	// Ref names the resource this field points at, for a foreign key like
	// userId. It is set only when a resource with a matching name was actually
	// found, so a dangling reference renders as a plain value rather than a
	// link that leads nowhere.
	Ref string
	// Image marks a field holding an image URL, which is worth showing as the
	// picture rather than as forty characters of URL.
	Image bool
}

// Model is every resource in a document.
type Model struct {
	Title     string
	Version   string
	BaseURL   string
	Resources []Resource
}

// itemSuffix matches a trailing path parameter: /users/{id} but not /users.
func itemSuffix(path string) (collection, param string, ok bool) {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "", "", false
	}
	last := path[i+1:]
	if !strings.HasPrefix(last, "{") || !strings.HasSuffix(last, "}") {
		return "", "", false
	}
	return path[:i], strings.TrimSuffix(strings.TrimPrefix(last, "{"), "}"), true
}

// Build groups a document's operations into resources.
//
// Grouping is by path, which is a convention rather than a guarantee — but it
// is the only signal a spec carries, and REST paths are conventional enough
// that the alternative (asking the operator to map 20 resources by hand) is
// not worth the accuracy it would buy.
func Build(doc *core.Document) Model {
	m := Model{Title: doc.Info.Title, Version: doc.Info.Version}
	if len(doc.Servers) > 0 {
		m.BaseURL = doc.Servers[0].URL
	}

	byCollection := map[string]*Resource{}
	get := func(path string) *Resource {
		if r, ok := byCollection[path]; ok {
			return r
		}
		r := &Resource{CollectionPath: path, Name: slug(path)}
		byCollection[path] = r
		return r
	}

	for path, methods := range doc.Paths {
		collection, param, isItem := itemSuffix(path)
		for method, op := range methods {
			// A realtime endpoint has no body to tabulate and no form to fill;
			// showing it as a resource would misrepresent it entirely.
			if op.Realtime != "" {
				continue
			}
			a := action(doc, method, path, op)
			if isItem {
				r := get(collection)
				r.ItemPath, r.IDParam = path, param
				switch strings.ToUpper(method) {
				case "GET":
					r.Get = a
				case "PUT", "PATCH":
					r.Update = a
				case "DELETE":
					r.Delete = a
				}
				continue
			}
			r := get(path)
			switch strings.ToUpper(method) {
			case "GET":
				r.List = a
			case "POST":
				r.Create = a
			}
		}
	}

	for _, r := range byCollection {
		// A collection with no list has nothing to be the master view of. It
		// may still be a real endpoint — it is simply not a resource an admin
		// panel can present, and inventing an empty table for it would suggest
		// the data is there when it never was.
		if r.List == nil {
			continue
		}
		r.Label, r.Singular = names(r)
		r.Fields = listFields(doc, r)
		r.Form = formFields(doc, r)
		m.Resources = append(m.Resources, *r)
	}
	sort.Slice(m.Resources, func(i, j int) bool { return m.Resources[i].Label < m.Resources[j].Label })
	link(m.Resources)
	return m
}

// link resolves foreign keys between resources. It runs after every resource is
// known, because a field can point at a resource defined anywhere in the
// document.
//
// The rule is deliberately narrow: a field named userId links to the resource
// whose item is a "user". Anything looser — matching on any shared substring,
// say — would turn fields like categoryPath or orderStatus into links to the
// wrong place, and a link that goes somewhere plausible but wrong is worse than
// no link at all.
func link(resources []Resource) {
	// A resource is findable by its singular ("user"), its plural ("users"),
	// and its last path segment, since APIs are not consistent about which of
	// those a foreign key is named after.
	byName := map[string]string{}
	for _, r := range resources {
		for _, key := range []string{r.Singular, r.Label, lastSegment(r.CollectionPath)} {
			if key = strings.ToLower(key); key != "" {
				byName[key] = r.Name
			}
		}
	}

	for i := range resources {
		for _, fields := range [][]Field{resources[i].Fields, resources[i].Form} {
			for j := range fields {
				f := &fields[j]
				f.Image = looksLikeImage(f.Name, f.Format)
				// The item's own id is not a link to itself.
				if f.Primary {
					continue
				}
				target, ok := foreignKey(f.Name)
				if !ok {
					continue
				}
				if name, found := byName[target]; found && name != resources[i].Name {
					f.Ref = name
				}
			}
		}
	}
}

// foreignKey reports the entity a field name refers to: userId -> "user",
// product_id -> "product". A bare "id" is the item's own identifier and refers
// to nothing.
func foreignKey(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, suffix := range []string{"_id", "id"} {
		if !strings.HasSuffix(lower, suffix) || len(lower) == len(suffix) {
			continue
		}
		return strings.TrimSuffix(strings.TrimSuffix(lower, suffix), "_"), true
	}
	return "", false
}

// imageHints are the names a field holding a picture actually has.
var imageHints = []string{"image", "photo", "picture", "avatar", "thumbnail", "thumb", "logo", "cover", "banner", "icon"}

func looksLikeImage(name, format string) bool {
	lower := strings.ToLower(name)
	for _, hint := range imageHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	// A bare uri format is not enough on its own: a homepage or a callback URL
	// is also a uri, and rendering either as a broken image helps nobody.
	return false
}

func action(doc *core.Document, method, path string, op *core.Operation) *Action {
	a := &Action{
		Method:  strings.ToUpper(method),
		Path:    path,
		Summary: op.Summary,
		Secured: len(op.Security) > 0,
	}
	// The operation id is the handler's own name, which is why a POST to
	// /carts reads "openCart" rather than a generic "create". That is the
	// team's vocabulary and it belongs on the button.
	a.Handler = op.OperationID
	a.Label = humanize(a.Handler)
	for _, p := range op.Parameters {
		if r := resolveParam(doc, p); r != nil && r.In == "header" && r.Required {
			a.Headers = append(a.Headers, r.Name)
		}
	}
	sort.Strings(a.Headers)
	return a
}

func resolveParam(doc *core.Document, p core.Parameter) *core.Parameter {
	if p.Ref == "" {
		return &p
	}
	key := strings.TrimPrefix(p.Ref, "#/components/parameters/")
	return doc.Components.Parameters[key]
}

// names picks what the operator reads. The handler names are the team's own
// vocabulary — a POST to /carts called openCart is "Open cart", not "Create
// cart" — so they win over anything derived from the path.
func names(r *Resource) (label, singular string) {
	// The collection label comes from the path, not the handler. A path
	// segment is the API's own plural and is spelled correctly — /categories —
	// whereas a handler called listCategorys would put "Categorys" in the
	// sidebar. Handler names still drive the action labels, where their
	// wording is the point.
	label = humanize(lastSegment(r.CollectionPath))
	singular = strings.TrimSuffix(label, "s")
	if r.Get != nil {
		if s := strings.TrimSpace(stripVerb(r.Get.Handler, "get", "show", "find", "fetch")); s != "" {
			singular = titleize(s)
		}
	}
	return label, singular
}

func stripVerb(name string, verbs ...string) string {
	lower := strings.ToLower(name)
	for _, v := range verbs {
		if strings.HasPrefix(lower, v) && len(name) > len(v) {
			return name[len(v):]
		}
	}
	return ""
}

func lastSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return parts[len(parts)-1]
}

func slug(path string) string {
	s := strings.Trim(path, "/")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.NewReplacer("{", "", "}", "").Replace(s)
	return s
}

// deref follows a $ref into components, and unwraps an array to its item.
func deref(doc *core.Document, s *core.Schema) *core.Schema {
	for i := 0; s != nil && i < 10; i++ {
		if s.Ref != "" {
			s = doc.Components.Schemas[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
			continue
		}
		if s.Type == "array" && s.Items != nil {
			s = s.Items
			continue
		}
		return s
	}
	return s
}

// bodySchema returns the JSON schema of an operation's success response.
func bodySchema(doc *core.Document, op *core.Operation) *core.Schema {
	if op == nil {
		return nil
	}
	for _, code := range []string{"200", "201", "202"} {
		resp, ok := op.Responses[code]
		if !ok || resp == nil {
			continue
		}
		if mt, ok := resp.Content["application/json"]; ok {
			return deref(doc, mt.Schema)
		}
	}
	return nil
}

func listFields(doc *core.Document, r *Resource) []Field {
	s := bodySchema(doc, opOf(doc, r.List))
	// A list often wraps its items ({"items": [...]}); the fields worth
	// tabulating are the item's, not the envelope's.
	if s != nil && s.Properties != nil {
		for _, key := range []string{"items", "data", "results", "list"} {
			if inner, ok := s.Properties[key]; ok {
				if d := deref(doc, inner); d != nil && d.Properties != nil {
					s = d
					break
				}
			}
		}
	}
	if s == nil {
		s = bodySchema(doc, opOf(doc, r.Get))
	}
	return fieldsOf(doc, s, 8)
}

func formFields(doc *core.Document, r *Resource) []Field {
	for _, a := range []*Action{r.Create, r.Update} {
		op := opOf(doc, a)
		if op == nil || op.RequestBody == nil {
			continue
		}
		if mt, ok := op.RequestBody.Content["application/json"]; ok {
			if f := fieldsOf(doc, deref(doc, mt.Schema), 0); len(f) > 0 {
				return f
			}
		}
	}
	return nil
}

func opOf(doc *core.Document, a *Action) *core.Operation {
	if a == nil {
		return nil
	}
	return doc.Paths[a.Path][strings.ToLower(a.Method)]
}

// fieldsOf flattens a schema's own properties. Nested objects are named but not
// expanded: a table cell showing a whole sub-object is unreadable, and the
// detail view renders them as JSON instead.
//
// limit caps how many columns a master table gets; 0 means all of them.
func fieldsOf(doc *core.Document, s *core.Schema, limit int) []Field {
	if s == nil || len(s.Properties) == 0 {
		return nil
	}
	required := map[string]bool{}
	for _, name := range s.Required {
		required[name] = true
	}

	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	// An identifier belongs first, wherever it sorted.
	sort.SliceStable(names, func(i, j int) bool { return isID(names[i]) && !isID(names[j]) })

	out := make([]Field, 0, len(names))
	for _, name := range names {
		p := s.Properties[name]
		f := Field{Name: name, Label: humanize(name), Required: required[name], Primary: isID(name)}
		if p != nil {
			f.Type, f.Format = schemaType(doc, p), p.Format
			for _, e := range p.Enum {
				if str, ok := e.(string); ok {
					f.Enum = append(f.Enum, str)
				}
			}
		}
		out = append(out, f)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// schemaType reports a property's JSON type, following a $ref to find it.
//
// Without this a property like `price` — a $ref to Money — has no type at all,
// and a form would render it as a text box and submit the string "" where the
// API expects an object. That is a 400 the operator cannot diagnose, so the
// type has to be resolved rather than left blank.
func schemaType(doc *core.Document, s *core.Schema) string {
	if s == nil {
		return ""
	}
	if s.Type != "" {
		return s.Type
	}
	if s.Ref != "" {
		if target := deref(doc, s); target != nil && target.Type != "" {
			return target.Type
		}
		// A $ref that resolves to something without a stated type is still a
		// composite; treating it as one is what keeps the form honest.
		return "object"
	}
	if len(s.Properties) > 0 {
		return "object"
	}
	if len(s.AllOf) > 0 {
		return "object"
	}
	return ""
}

func isID(name string) bool {
	lower := strings.ToLower(name)
	return lower == "id" || lower == "uuid" || strings.HasSuffix(lower, "_id")
}

// humanize turns listUsers or created_at into something an operator reads.
func humanize(s string) string {
	if s == "" {
		return ""
	}
	s = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(s)

	var b strings.Builder
	for i, r := range s {
		// A capital starting a new word gets a space, except in a run of them
		// (ID, URL) where the run is one word.
		if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(rune(s[i-1])) && s[i-1] != ' ' {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	// Each word is capitalised, not just the first: these become column headers
	// and button labels, where "Created At" reads as a heading and "Created at"
	// reads as a truncated sentence.
	words := strings.Fields(b.String())
	for i, w := range words {
		words[i] = titleize(w)
	}
	return strings.Join(words, " ")
}

func titleize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Actions lists the per-row menu entries a resource supports. It exists so the
// template does not decide policy: if the API has no update endpoint, no Edit
// entry is produced, and there is no way for the markup to add one back.
func (r Resource) Actions() []Action {
	var out []Action
	for _, a := range []*Action{r.Get, r.Update, r.Delete} {
		if a != nil {
			out = append(out, *a)
		}
	}
	return out
}

// HasMenu reports whether a row needs a three-dots menu at all.
func (r Resource) HasMenu() bool { return len(r.Actions()) > 0 }
