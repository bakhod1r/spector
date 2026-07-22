// Package graphqlsdl reads GraphQL SDL sources (*.graphql, *.graphqls) and
// builds a core.GraphqlDoc from the type definitions and the fields exposed
// on the Query/Mutation/Subscription root types. It is the primary source
// used when a project ships hand-written or generated schema files.
package graphqlsdl

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"

	"github.com/user/specter/internal/core"
)

// Scan walks dir for *.graphql/*.graphqls files and builds a GraphqlDoc from
// their type and root-field definitions.
func Scan(dir string) (*core.GraphqlDoc, error) {
	files, err := schemaFiles(dir)
	if err != nil {
		return nil, err
	}
	doc := core.NewGraphqlDoc()
	if len(files) == 0 {
		return doc, nil
	}

	var sources []*ast.Source
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, &ast.Source{Name: path, Input: string(data)})
	}

	schema, err := parser.ParseSchemas(sources...)
	if err != nil {
		return nil, err
	}

	for _, def := range schema.Definitions {
		switch def.Kind {
		case ast.Object, ast.InputObject, ast.Interface:
			switch def.Name {
			case "Query":
				doc.Queries = append(doc.Queries, rootFields(def.Fields)...)
			case "Mutation":
				doc.Mutations = append(doc.Mutations, rootFields(def.Fields)...)
			case "Subscription":
				doc.Subscriptions = append(doc.Subscriptions, rootFields(def.Fields)...)
			default:
				doc.Types[def.Name] = objectSchema(def)
			}
		case ast.Enum:
			if doc.Enums == nil {
				doc.Enums = map[string]*core.Schema{}
			}
			doc.Enums[def.Name] = enumSchema(def)
		}
	}
	return doc, nil
}

func schemaFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".graphql") || strings.HasSuffix(path, ".graphqls") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func rootFields(fields ast.FieldList) []*core.GraphqlField {
	out := make([]*core.GraphqlField, 0, len(fields))
	for _, f := range fields {
		field := &core.GraphqlField{
			Name:        f.Name,
			ReturnType:  f.Type.String(),
			Description: f.Description,
		}
		for _, arg := range f.Arguments {
			field.Args = append(field.Args, &core.GraphqlArg{Name: arg.Name, Type: arg.Type.String()})
		}
		out = append(out, field)
	}
	return out
}

func objectSchema(def *ast.Definition) *core.Schema {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{}}
	for _, f := range def.Fields {
		schema.Properties[f.Name] = typeSchema(f.Type)
	}
	return schema
}

func enumSchema(def *ast.Definition) *core.Schema {
	schema := &core.Schema{Type: "string"}
	for _, v := range def.EnumValues {
		schema.Enum = append(schema.Enum, v.Name)
	}
	return schema
}

// typeSchema maps a GraphQL type reference to a JSON Schema fragment,
// resolving named scalars and referencing other types by name.
func typeSchema(t *ast.Type) *core.Schema {
	if t.Elem != nil {
		return &core.Schema{Type: "array", Items: typeSchema(t.Elem)}
	}
	switch t.NamedType {
	case "String", "ID":
		return &core.Schema{Type: "string"}
	case "Int":
		return &core.Schema{Type: "integer"}
	case "Float":
		return &core.Schema{Type: "number"}
	case "Boolean":
		return &core.Schema{Type: "boolean"}
	default:
		return &core.Schema{Ref: "#/components/schemas/" + t.NamedType}
	}
}
