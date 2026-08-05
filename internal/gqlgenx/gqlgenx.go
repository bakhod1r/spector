// Package gqlgenx reads GraphQL resolvers from gqlgen-generated Go code
// (models_gen.go, generated.go, resolver interfaces) rather than from .graphql
// sources. It is the fallback used when a project ships generated Go stubs
// but not the original schema files.
package gqlgenx

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/specter/internal/core"
)

// Scan walks dir for *.go files and reconstructs a GraphQL document from the
// QueryResolver/MutationResolver/SubscriptionResolver interfaces and the
// model structs they reference.
func Scan(dir string) (*core.GraphqlDoc, error) {
	files, err := parseFiles(dir)
	if err != nil {
		return nil, err
	}

	scanner := core.NewStructScanner()
	for _, file := range files {
		scanner.Collect(file)
	}

	doc := core.NewGraphqlDoc()
	used := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				fields := resolverFields(it, scanner.Schemas, used)
				switch {
				case strings.HasPrefix(ts.Name.Name, "Query"):
					doc.Queries = append(doc.Queries, fields...)
				case strings.HasPrefix(ts.Name.Name, "Mutation"):
					doc.Mutations = append(doc.Mutations, fields...)
				case strings.HasPrefix(ts.Name.Name, "Subscription"):
					doc.Subscriptions = append(doc.Subscriptions, fields...)
				}
			}
		}
	}

	for name := range used {
		if s := scanner.Schemas[name]; s != nil {
			doc.Types[name] = s
		}
	}
	return doc, nil
}

func parseFiles(dir string) ([]*ast.File, error) {
	var files []*ast.File
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		files = append(files, f)
		return nil
	})
	return files, err
}

// resolverFields extracts each exported method of a QueryResolver-style
// interface as a GraphqlField, resolving argument/return type names and
// pulling any referenced model schemas into used.
func resolverFields(it *ast.InterfaceType, schemas map[string]*core.Schema, used map[string]bool) []*core.GraphqlField {
	var out []*core.GraphqlField
	for _, m := range it.Methods.List {
		if len(m.Names) == 0 || !m.Names[0].IsExported() {
			continue
		}
		ft, ok := m.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		field := &core.GraphqlField{Name: m.Names[0].Name}
		if params := ft.Params; params != nil {
			for _, p := range params.List {
				info := parseGQLType(p.Type)
				if info.name == "" || info.name == "Context" {
					continue
				}
				names := p.Names
				if len(names) == 0 {
					names = []*ast.Ident{{Name: info.name}}
				}
				for _, n := range names {
					field.Args = append(field.Args, &core.GraphqlArg{Name: n.Name, Type: typeRef(info)})
				}
				collect(info.name, schemas, used)
			}
		}
		if ft.Results != nil {
			for _, r := range ft.Results.List {
				info := parseGQLType(r.Type)
				if info.name == "" || info.name == "error" {
					continue
				}
				field.ReturnType = typeRef(info)
				collect(info.name, schemas, used)
				break
			}
		}
		out = append(out, field)
	}
	return out
}

// gqlType is a Go type decomposed the way gqlgen maps to GraphQL nullability: a
// pointer is a nullable GraphQL type, a value a non-null one. The distinction is
// tracked separately for the list and its element, so []*User (non-null list of
// nullable users) is not confused with []User.
type gqlType struct {
	name     string
	array    bool
	nullable bool // the outer type (scalar, or the list itself) is a pointer
	elemNull bool // list elements are pointers
}

// parseGQLType decomposes a Go type expression, recording pointer-ness at each
// level. gqlgen models emit T for a non-null field and *T for a nullable one, so
// pointer-ness is the only place the GraphQL ! survives once the SDL is gone.
func parseGQLType(expr ast.Expr) gqlType {
	switch t := expr.(type) {
	case *ast.StarExpr:
		inner := parseGQLType(t.X)
		inner.nullable = true // this pointer makes the current outer level nullable
		return inner
	case *ast.ArrayType:
		elem := parseGQLType(t.Elt)
		return gqlType{name: elem.name, array: true, elemNull: elem.nullable}
	case *ast.Ident:
		return gqlType{name: t.Name}
	case *ast.SelectorExpr:
		return gqlType{name: t.Sel.Name}
	}
	return gqlType{}
}

// typeRef renders the decomposed type as a GraphQL type reference, appending !
// wherever the Go type was a value rather than a pointer: User (non-null) reads
// User!, *User reads User, []*User reads [User]!, and []User reads [User!]!.
func typeRef(info gqlType) string {
	if info.array {
		elem := info.name
		if !info.elemNull {
			elem += "!"
		}
		list := "[" + elem + "]"
		if !info.nullable {
			list += "!"
		}
		return list
	}
	name := info.name
	if !info.nullable {
		name += "!"
	}
	return name
}

func collect(name string, all map[string]*core.Schema, used map[string]bool) {
	if name == "" || used[name] {
		return
	}
	schema := all[name]
	if schema == nil {
		return
	}
	used[name] = true
	walk(schema, all, used)
}

const refPrefix = "#/components/schemas/"

func walk(schema *core.Schema, all map[string]*core.Schema, used map[string]bool) {
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		collect(strings.TrimPrefix(schema.Ref, refPrefix), all, used)
	}
	if schema.Items != nil {
		walk(schema.Items, all, used)
	}
	if schema.AdditionalProperties != nil {
		walk(schema.AdditionalProperties, all, used)
	}
	for _, p := range schema.Properties {
		walk(p, all, used)
	}
}
