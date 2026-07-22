package core

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// StructScanner walks Go source files and turns struct declarations into
// OpenAPI schemas keyed by the Go type name.
type StructScanner struct {
	Schemas map[string]*Schema
}

// NewStructScanner returns an empty scanner.
func NewStructScanner() *StructScanner {
	return &StructScanner{Schemas: map[string]*Schema{}}
}

// Collect visits a parsed file and records every top-level struct type,
// plus Go "enum" types: a named basic type (e.g. `type Status string`)
// paired with a set of typed const values. Enum types become string/integer
// schemas carrying an `enum` list, so a `$ref` to them documents the
// allowed values.
func (s *StructScanner) Collect(file *ast.File) {
	// First pass: record named basic types that may back an enum.
	enumKind := map[string]string{} // type name -> "string" | "integer"
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
			if id, ok := ts.Type.(*ast.Ident); ok {
				if kind := basicKind(id.Name); kind != "" {
					enumKind[ts.Name.Name] = kind
				}
			}
		}
	}

	// Second pass: structs, and const values feeding the enum types.
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gd.Tok {
		case token.TYPE:
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					s.Schemas[ts.Name.Name] = s.structToSchema(st)
				}
			}
		case token.CONST:
			s.collectEnumConsts(gd, enumKind)
		}
	}
}

// collectEnumConsts walks a const block and appends each constant to the
// enum schema of its named type. Both explicit values and iota-based
// declarations are supported: the type and value expression may appear only
// on the first spec of a group, carrying forward to later specs while iota
// increments with position (Go's const repetition rule).
func (s *StructScanner) collectEnumConsts(gd *ast.GenDecl, enumKind map[string]string) {
	current := ""         // enum type name in effect for this block
	var lastExpr ast.Expr // value expression to repeat when a spec omits one
	for iota, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if vs.Type != nil {
			if id, ok := vs.Type.(*ast.Ident); ok {
				if _, isEnum := enumKind[id.Name]; isEnum {
					current = id.Name
				} else {
					current = ""
				}
			} else {
				current = ""
			}
		}
		if len(vs.Values) > 0 {
			lastExpr = vs.Values[0]
		}
		if current == "" || lastExpr == nil {
			continue
		}
		val := evalConst(lastExpr, iota, enumKind[current])
		if val == nil {
			continue
		}
		sch := s.Schemas[current]
		if sch == nil {
			sch = &Schema{Type: enumKind[current]}
			s.Schemas[current] = sch
		}
		sch.Enum = append(sch.Enum, val)
	}
}

// evalConst resolves a const value expression for the given iota position and
// enum kind. It handles string/int literals and common iota expressions
// (iota, iota±N, N±iota, N<<iota). Returns nil when it can't be reduced.
func evalConst(expr ast.Expr, iota int, kind string) any {
	if kind == "string" {
		if lit, ok := expr.(*ast.BasicLit); ok {
			return litValue(lit, kind)
		}
		return nil
	}
	if n, ok := evalInt(expr, iota); ok {
		return n
	}
	return nil
}

// evalInt evaluates an integer const expression involving iota.
func evalInt(expr ast.Expr, iota int) (int, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "iota" {
			return iota, true
		}
	case *ast.BasicLit:
		if e.Kind == token.INT {
			if v, err := strconv.Atoi(e.Value); err == nil {
				return v, true
			}
		}
	case *ast.ParenExpr:
		return evalInt(e.X, iota)
	case *ast.BinaryExpr:
		l, lok := evalInt(e.X, iota)
		r, rok := evalInt(e.Y, iota)
		if !lok || !rok {
			return 0, false
		}
		switch e.Op {
		case token.ADD:
			return l + r, true
		case token.SUB:
			return l - r, true
		case token.MUL:
			return l * r, true
		case token.SHL:
			return l << uint(r), true
		case token.SHR:
			return l >> uint(r), true
		}
	}
	return 0, false
}

// basicKind maps a Go basic type name to the JSON Schema type used for an
// enum backed by it, or "" if it isn't a supported enum base.
func basicKind(name string) string {
	switch name {
	case "string":
		return "string"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "integer"
	}
	return ""
}

// litValue decodes a const literal into its Go value for the given kind.
func litValue(lit *ast.BasicLit, kind string) any {
	switch {
	case kind == "string" && lit.Kind == token.STRING:
		if v, err := strconv.Unquote(lit.Value); err == nil {
			return v
		}
	case kind == "integer" && lit.Kind == token.INT:
		if v, err := strconv.Atoi(lit.Value); err == nil {
			return v
		}
	}
	return nil
}

func (s *StructScanner) structToSchema(st *ast.StructType) *Schema {
	schema := &Schema{Type: "object", Properties: map[string]*Schema{}}
	for _, field := range st.Fields.List {
		// Embedded field (no field names). If it carries no json name it is
		// promoted: compose it via allOf with a $ref so its properties are
		// inherited. With an explicit json tag it behaves like a named field.
		if len(field.Names) == 0 {
			if name := embeddedJSONName(field); name != "" {
				schema.Properties[name] = exprToSchema(field.Type)
				continue
			}
			if ref := embeddedRef(field.Type); ref != "" {
				schema.AllOf = append(schema.AllOf, &Schema{Ref: refPrefix + ref})
			}
			continue
		}
		// One declaration may introduce several fields ("A, B string"); each of
		// them is serialized, so each needs documenting.
		for _, ident := range field.Names {
			if !ident.IsExported() {
				continue // unexported fields are never serialized (e.g. pb.go state)
			}
			name := jsonNameFor(field, ident)
			if name == "-" || name == "" {
				continue
			}
			prop := exprToSchema(field.Type)
			// Validation is applied after the type is known: min=3 means a
			// value bound on a number, a length on a string, and a count on an
			// array, so the rule cannot be interpreted without the type.
			if field.Tag != nil {
				tag := strings.Trim(field.Tag.Value, "`")
				applyDocTags(prop, tag)
				if applyValidation(prop, tag) {
					schema.Required = append(schema.Required, name)
				}
			}
			schema.Properties[name] = prop
		}
	}
	// An object whose only content is composed schemas needs no empty
	// properties map alongside allOf.
	if len(schema.Properties) == 0 && len(schema.AllOf) > 0 {
		schema.Properties = nil
		schema.Type = ""
	}
	return schema
}

const refPrefix = "#/components/schemas/"

// embeddedJSONName returns the json tag name of an embedded field, or "" when
// it has none (in which case the field is promoted rather than nested).
func embeddedJSONName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag := strings.Trim(field.Tag.Value, "`")
	idx := strings.Index(tag, `json:"`)
	if idx == -1 {
		return ""
	}
	val := tag[idx+len(`json:"`):]
	if end := strings.Index(val, `"`); end != -1 {
		val = val[:end]
	}
	if comma := strings.Index(val, ","); comma != -1 {
		val = val[:comma]
	}
	if val == "-" {
		return ""
	}
	return val
}

// embeddedRef resolves the referenced type name of an embedded field
// (T, *T, or pkg.T), or "" when it is not a named type.
func embeddedRef(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		if identSchema(t.Name).Ref != "" {
			return t.Name
		}
	case *ast.StarExpr:
		return embeddedRef(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// jsonName resolves a field's wire name from its json tag, falling back to
// the Go field name. Returns "-" when the field is explicitly skipped.
// jsonNameFor resolves the serialized name of one identifier in a field
// declaration, which may declare several.
func jsonNameFor(field *ast.Field, ident *ast.Ident) string {
	goName := ident.Name
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
	val := rest[:end]
	if comma := strings.Index(val, ","); comma != -1 {
		val = val[:comma]
	}
	if val == "" {
		return goName
	}
	return val
}

// exprToSchema maps a Go type expression to a JSON Schema fragment.
func exprToSchema(expr ast.Expr) *Schema {
	switch t := expr.(type) {
	case *ast.Ident:
		return identSchema(t.Name)
	case *ast.StarExpr: // *T -> same as T (nullable elided for simplicity)
		return exprToSchema(t.X)
	case *ast.ArrayType: // []T
		return &Schema{Type: "array", Items: exprToSchema(t.Elt)}
	case *ast.SelectorExpr: // time.Time etc.
		if t.Sel.Name == "Time" {
			return &Schema{Type: "string", Format: "date-time"}
		}
		return &Schema{Type: "object"}
	case *ast.MapType:
		return &Schema{Type: "object", AdditionalProperties: exprToSchema(t.Value)}
	default:
		return &Schema{Type: "object"}
	}
}

func identSchema(name string) *Schema {
	switch name {
	case "string":
		return &Schema{Type: "string"}
	case "bool":
		return &Schema{Type: "boolean"}
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return &Schema{Type: "integer"}
	case "float32", "float64":
		return &Schema{Type: "number"}
	default:
		// Assume it's a named struct type -> reference it.
		return &Schema{Ref: "#/components/schemas/" + name}
	}
}
