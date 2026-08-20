package astutil

import (
	"go/ast"
	"go/token"
	"path/filepath"
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

// StructIndex is StructFields and StructFieldTypes arranged by the package a
// struct was declared in.
//
// A tree-wide table keyed by name alone is wrong for the same reason a
// tree-wide function table was: `Handler`, `Envelope` and `Response` are each
// declared once per bounded context, and the first one parsed answered for all
// of them. One context's handler then read its own state through another
// context's field list and found nothing, so the endpoint documented no
// payload.
type StructIndex struct {
	wire  map[string]map[string]map[string]string
	types map[string]map[string]map[string]TypeInfo
}

// NewStructIndex reads every struct declaration, grouped by directory.
func NewStructIndex(fset *token.FileSet, files []*ast.File) *StructIndex {
	ix := &StructIndex{
		wire:  map[string]map[string]map[string]string{},
		types: map[string]map[string]map[string]TypeInfo{},
	}
	for _, f := range files {
		dir := filepath.Dir(fset.Position(f.Pos()).Filename)
		one := []*ast.File{f}
		for name, fields := range StructFields(one) {
			if ix.wire[dir] == nil {
				ix.wire[dir] = map[string]map[string]string{}
			}
			if _, taken := ix.wire[dir][name]; !taken {
				ix.wire[dir][name] = fields
			}
		}
		for name, fields := range StructFieldTypes(one) {
			if ix.types[dir] == nil {
				ix.types[dir] = map[string]map[string]TypeInfo{}
			}
			if _, taken := ix.types[dir][name]; !taken {
				ix.types[dir][name] = fields
			}
		}
	}
	return ix
}

// Wire is the serialized name of each field of a struct declared in dir.
func (ix *StructIndex) Wire(dir, name string) map[string]string {
	if ix == nil {
		return nil
	}
	return ix.wire[dir][name]
}

// Elems is the element type behind each indexable field of a struct declared
// in dir.
func (ix *StructIndex) Elems(dir, name string) map[string]TypeInfo {
	if ix == nil {
		return nil
	}
	return ix.types[dir][name]
}

// WireAnywhere falls back to a struct of this name declared anywhere, for a
// type reached across a package boundary — a shared envelope in a helper
// package is the normal case. A name declared in several packages resolves to
// none of them rather than to the wrong one.
func (ix *StructIndex) WireAnywhere(name string) map[string]string {
	if ix == nil {
		return nil
	}
	var found map[string]string
	for _, byName := range ix.wire {
		if fields, ok := byName[name]; ok {
			if found != nil {
				return nil
			}
			found = fields
		}
	}
	return found
}
