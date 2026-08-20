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
