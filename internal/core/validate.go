package core

import (
	"strconv"
	"strings"
)

// Validation rules are written in two tags that share one syntax:
// gin reads `binding:"..."` and go-playground/validator reads `validate:"..."`.
// One parser serves both.
const (
	bindingTag  = "binding"
	validateTag = "validate"
)

// applyDocTags reads the tags that carry documentation rather than rules:
// `doc:"..."` for a description and `example:"..."` for a sample value.
//
// Huma and several other Go API libraries established these spellings, so a
// project migrating to Specter keeps what it already wrote. They constrain
// nothing, which is why they are read unconditionally: unlike a bound, a
// description cannot make the document wrong.
//
// The example is typed to match the field, so an integer field does not end up
// documented with the string "25".
func applyDocTags(schema *Schema, tag string) {
	if schema == nil {
		return
	}
	if doc := tagValue(tag, "doc"); doc != "" {
		schema.Description = doc
	}
	if ex := tagValue(tag, "example"); ex != "" {
		schema.Example = typedExample(schema.Type, ex)
	}
}

// typedExample converts the raw tag text to the field's type. A value that does
// not parse is kept as a string rather than dropped: it is still a better hint
// to a reader than nothing, and examples are not validated.
func typedExample(schemaType, raw string) any {
	switch schemaType {
	case "integer":
		if n, ok := atoi(raw); ok {
			return n
		}
	case "number":
		if f, ok := atof(raw); ok {
			return f
		}
	case "boolean":
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true":
			return true
		case "false":
			return false
		}
	}
	return raw
}

// applyValidation reads the validation tag on a field and constrains the
// schema accordingly. It reports whether the field is required, since that
// belongs on the parent object rather than on the property.
//
// Only rules with a direct JSON Schema equivalent are translated. Everything
// else — gtfield, required_with, contains, custom validators — is ignored in
// silence: those constraints are real, but there is nowhere in the schema to
// put them, and a document that stops generating because of an unrecognised
// rule would be worse than one that omits it.
//
// A malformed tag is treated the same way. `gte=` with no number is a typo in
// someone's struct, and refusing to document their API is not a proportionate
// response to it.
func applyValidation(schema *Schema, tag string) (required bool) {
	if schema == nil {
		return false
	}
	rules := validationRules(tag)
	if len(rules) == 0 {
		return false
	}

	for _, rule := range rules {
		name, arg, _ := strings.Cut(rule, "=")
		switch name {
		case "required":
			required = true
		case "omitempty", "-":
			// Not a constraint; it governs serialization, not validity.
		case "gte":
			setMin(schema, arg, false)
		case "gt":
			setMin(schema, arg, true)
		case "lte":
			setMax(schema, arg, false)
		case "lt":
			setMax(schema, arg, true)
		case "min":
			applyBound(schema, arg, true)
		case "max":
			applyBound(schema, arg, false)
		case "len":
			applyExactLength(schema, arg)
		case "oneof":
			applyOneOf(schema, arg)
		default:
			if f, ok := formats[name]; ok && schema.Format == "" {
				schema.Format = f
			}
		}
	}
	return required
}

// formats are the rules that name a string format JSON Schema already has a
// word for. A format is descriptive, so setting one never makes a document
// wrong the way a bad bound would.
var formats = map[string]string{
	"email":    "email",
	"url":      "uri",
	"uri":      "uri",
	"uuid":     "uuid",
	"uuid4":    "uuid",
	"ipv4":     "ipv4",
	"ipv6":     "ipv6",
	"ip":       "ip",
	"datetime": "date-time",
	"hostname": "hostname",
}

// validationRules extracts the comma-separated rules from whichever validation
// tag the field carries. binding wins when both are present, since a gin
// project that writes both means the binding one.
func validationRules(tag string) []string {
	raw := tagValue(tag, bindingTag)
	if raw == "" {
		raw = tagValue(tag, validateTag)
	}
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// tagValue pulls one key's value out of a struct tag. The tags Specter reads
// are simple enough that reflect.StructTag's parser is not needed, and this
// works on the raw text the AST gives us.
func tagValue(tag, key string) string {
	idx := strings.Index(tag, key+`:"`)
	if idx == -1 {
		return ""
	}
	rest := tag[idx+len(key)+2:]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}
	return rest[:end]
}

// applyBound routes min/max to the right constraint, which depends entirely on
// the field's type: `min=3` on a number is a value bound, on a string a length,
// on an array a count. This is why validation is applied after the type is
// resolved rather than while the tag is read.
func applyBound(schema *Schema, arg string, isMin bool) {
	switch schema.Type {
	case "integer", "number":
		if isMin {
			setMin(schema, arg, false)
		} else {
			setMax(schema, arg, false)
		}
	case "string":
		if n, ok := atoi(arg); ok {
			if isMin {
				schema.MinLength = &n
			} else {
				schema.MaxLength = &n
			}
		}
	case "array":
		if n, ok := atoi(arg); ok {
			if isMin {
				schema.MinItems = &n
			} else {
				schema.MaxItems = &n
			}
		}
	case "object":
		// A map's min/max counts entries. JSON Schema spells that
		// minProperties, which is not modelled, so it is dropped rather than
		// mistranslated into minItems.
	}
}

func applyExactLength(schema *Schema, arg string) {
	n, ok := atoi(arg)
	if !ok {
		return
	}
	switch schema.Type {
	case "string":
		schema.MinLength, schema.MaxLength = &n, &n
	case "array":
		schema.MinItems, schema.MaxItems = &n, &n
	case "integer", "number":
		// len on a number is not a length; validator treats it as equality.
		f := float64(n)
		schema.Minimum, schema.Maximum = &f, &f
	}
}

// applyOneOf turns `oneof=a b c` into an enum. The values are typed to match
// the field so the document does not claim an integer field accepts strings.
func applyOneOf(schema *Schema, arg string) {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return
	}
	var enum []any
	for _, p := range parts {
		switch schema.Type {
		case "integer":
			n, err := strconv.Atoi(p)
			if err != nil {
				return // a non-numeric value on a numeric field: trust neither
			}
			enum = append(enum, n)
		case "number":
			f, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return
			}
			enum = append(enum, f)
		default:
			enum = append(enum, p)
		}
	}
	// An enum discovered from typed constants is more precise than one written
	// in a tag, so it is not overwritten.
	if len(schema.Enum) == 0 {
		schema.Enum = enum
	}
}

func setMin(schema *Schema, arg string, exclusive bool) {
	if f, ok := atof(arg); ok {
		schema.Minimum = &f
		schema.ExclusiveMinimum = exclusive
	}
}

func setMax(schema *Schema, arg string, exclusive bool) {
	if f, ok := atof(arg); ok {
		schema.Maximum = &f
		schema.ExclusiveMaximum = exclusive
	}
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

func atof(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
