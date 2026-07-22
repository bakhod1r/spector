package core

// GraphqlDoc is the documented shape of a GraphQL schema: its object/input
// types, enums, and the fields exposed on the Query/Mutation/Subscription
// root types.
type GraphqlDoc struct {
	Types         map[string]*Schema `json:"types"`
	Enums         map[string]*Schema `json:"enums,omitempty"`
	Queries       []*GraphqlField    `json:"queries"`
	Mutations     []*GraphqlField    `json:"mutations,omitempty"`
	Subscriptions []*GraphqlField    `json:"subscriptions,omitempty"`
}

// GraphqlField is a single field on a Query/Mutation/Subscription root type.
type GraphqlField struct {
	Name        string        `json:"name"`
	Args        []*GraphqlArg `json:"args,omitempty"`
	ReturnType  string        `json:"returnType"`
	Description string        `json:"description,omitempty"`
}

// GraphqlArg is a single argument of a GraphqlField.
type GraphqlArg struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// NewGraphqlDoc returns an empty-but-initialized document.
func NewGraphqlDoc() *GraphqlDoc {
	return &GraphqlDoc{
		Types:   map[string]*Schema{},
		Queries: []*GraphqlField{},
	}
}
