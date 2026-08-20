package astutil

import (
	"go/ast"
	"strings"
)

// StructFields indexes, per struct type, the serialized name of each Go field.
//
// An envelope's payload field is found by name — `Envelope.Data` — but the
// schema that has to be rewritten is keyed by the wire name the json tag gives
// it. Matching on the schema alone is not possible: `Data any` renders as a
// bare object and so does any other struct-valued field, so the rewrite would
// have to guess which property is the payload.
func StructFields(files []*ast.File) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				fields := map[string]string{}
				for _, field := range st.Fields.List {
					for _, id := range field.Names {
						if name := wireName(field, id.Name); name != "" {
							fields[id.Name] = name
						}
					}
				}
				if len(fields) > 0 {
					// The first declaration of a name wins, as it does
					// everywhere else in the scan, so the result does not
					// depend on walk order.
					if _, taken := out[ts.Name.Name]; !taken {
						out[ts.Name.Name] = fields
					}
				}
			}
		}
	}
	return out
}

// wireName is the json name of a field, or "" when the field is not
// serialized.
func wireName(field *ast.Field, goName string) string {
	if field.Tag == nil {
		return goName
	}
	tag := strings.Trim(field.Tag.Value, "`")
	idx := strings.Index(tag, `json:"`)
	if idx == -1 {
		return goName
	}
	rest := tag[idx+len(`json:"`):]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return goName
	}
	name, _, _ := strings.Cut(rest[:end], ",")
	switch name {
	case "-":
		return ""
	case "":
		return goName
	}
	return name
}

// StructFieldTypes indexes, per struct type, the Go type of each field.
//
// A handler that answers out of its own state — `u, ok := h.users[id]` — names
// the payload type nowhere in its body: the type lives on the receiver's field
// declaration. Without it such an endpoint documents no response at all, and
// keeping a store on the handler is how most services are written.
func StructFieldTypes(files []*ast.File) map[string]map[string]TypeInfo {
	out := map[string]map[string]TypeInfo{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				if _, taken := out[ts.Name.Name]; taken {
					continue
				}
				fields := map[string]TypeInfo{}
				for _, field := range st.Fields.List {
					info, ok := containerElem(field.Type)
					if !ok {
						continue
					}
					for _, id := range field.Names {
						fields[id.Name] = info
					}
				}
				if len(fields) > 0 {
					out[ts.Name.Name] = fields
				}
			}
		}
	}
	return out
}

// containerElem is the element type behind a field an index expression reads:
// a map's value or a slice's element. Anything else reports false — indexing
// it is not how a payload is produced.
func containerElem(expr ast.Expr) (TypeInfo, bool) {
	switch t := expr.(type) {
	case *ast.MapType:
		info := TypeName(t.Value)
		return info, info.Name != ""
	case *ast.ArrayType:
		info := TypeName(t.Elt)
		return info, info.Name != ""
	}
	return TypeInfo{}, false
}
