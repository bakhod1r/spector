package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/graphql-go/graphql"
)

// This resolves the schema in examples/shop/graphql/schema.graphql so the
// console's GraphQL tab has a real endpoint to execute against. The data is
// seeded in memory, like the REST handlers above it.

var (
	gqlUsers = []map[string]any{
		{"id": "1", "name": "Ada", "email": "ada@example.com", "roles": []any{"admin"}},
		{"id": "2", "name": "Alan", "email": "alan@example.com", "roles": []any{"user"}},
		{"id": "3", "name": "Grace", "email": "grace@example.com", "roles": []any{"user", "editor"}},
	}
	gqlProducts = []map[string]any{
		{"id": "1", "name": "Widget", "price": map[string]any{"amount": 9.99, "currency": "USD"},
			"tags": []any{"tools"}, "inStock": true},
		{"id": "2", "name": "Gadget", "price": map[string]any{"amount": 24.5, "currency": "USD"},
			"tags": []any{"tools", "new"}, "inStock": false},
	}
)

// orNil converts a nil map into a nil interface. Returning a nil map directly
// yields a non-nil `any` holding a nil value, which graphql-go reads as a
// present object and then fails on its non-null fields.
func orNil(row map[string]any) any {
	if row == nil {
		return nil
	}
	return row
}

func findByID(rows []map[string]any, id string) map[string]any {
	for _, r := range rows {
		if r["id"] == id {
			return r
		}
	}
	return nil
}

var moneyType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Money",
	Fields: graphql.Fields{
		"amount":   &graphql.Field{Type: graphql.NewNonNull(graphql.Float)},
		"currency": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
	},
})

var orderStatusEnum = graphql.NewEnum(graphql.EnumConfig{
	Name: "OrderStatus",
	Values: graphql.EnumValueConfigMap{
		"PENDING":   &graphql.EnumValueConfig{Value: "PENDING"},
		"PAID":      &graphql.EnumValueConfig{Value: "PAID"},
		"SHIPPED":   &graphql.EnumValueConfig{Value: "SHIPPED"},
		"CANCELLED": &graphql.EnumValueConfig{Value: "CANCELLED"},
	},
})

var userType = graphql.NewObject(graphql.ObjectConfig{
	Name: "User",
	Fields: graphql.Fields{
		"id":    &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
		"name":  &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		"email": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		"roles": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.String)))},
	},
})

var productType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Product",
	Fields: graphql.Fields{
		"id":      &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
		"name":    &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		"price":   &graphql.Field{Type: graphql.NewNonNull(moneyType)},
		"tags":    &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.String)))},
		"inStock": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
	},
})

var orderLineType = graphql.NewObject(graphql.ObjectConfig{
	Name: "OrderLine",
	Fields: graphql.Fields{
		"product":  &graphql.Field{Type: graphql.NewNonNull(productType)},
		"quantity": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
	},
})

var orderType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Order",
	Fields: graphql.Fields{
		"id":     &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
		"user":   &graphql.Field{Type: graphql.NewNonNull(userType)},
		"lines":  &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(orderLineType)))},
		"total":  &graphql.Field{Type: graphql.NewNonNull(moneyType)},
		"status": &graphql.Field{Type: graphql.NewNonNull(orderStatusEnum)},
	},
})

var newOrderLineInput = graphql.NewInputObject(graphql.InputObjectConfig{
	Name: "NewOrderLine",
	Fields: graphql.InputObjectConfigFieldMap{
		"productId": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.ID)},
		"quantity":  &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.Int)},
	},
})

var placeOrderInput = graphql.NewInputObject(graphql.InputObjectConfig{
	Name: "PlaceOrderInput",
	Fields: graphql.InputObjectConfigFieldMap{
		"userId": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.ID)},
		"lines":  &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(newOrderLineInput)))},
	},
})

func buildOrder(userID string, lines []any) map[string]any {
	user := findByID(gqlUsers, userID)
	if user == nil {
		user = gqlUsers[0]
	}
	out := []any{}
	total := 0.0
	for _, raw := range lines {
		line, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		productID, _ := line["productId"].(string)
		product := findByID(gqlProducts, productID)
		if product == nil {
			continue
		}
		qty, _ := line["quantity"].(int)
		price := product["price"].(map[string]any)["amount"].(float64)
		total += price * float64(qty)
		out = append(out, map[string]any{"product": product, "quantity": qty})
	}
	return map[string]any{
		"id": "1001", "user": user, "lines": out,
		"total":  map[string]any{"amount": total, "currency": "USD"},
		"status": "PENDING",
	}
}

func newGraphqlSchema() (graphql.Schema, error) {
	query := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"user": &graphql.Field{
				Type:        userType,
				Description: "Fetch a single user by id.",
				Args:        graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, _ := p.Args["id"].(string)
					return orNil(findByID(gqlUsers, id)), nil
				},
			},
			"users": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(userType))),
				Description: "List users, newest first.",
				Args:        graphql.FieldConfigArgument{"limit": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 20}},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					limit, _ := p.Args["limit"].(int)
					if limit <= 0 || limit > len(gqlUsers) {
						limit = len(gqlUsers)
					}
					return gqlUsers[:limit], nil
				},
			},
			"product": &graphql.Field{
				Type:        productType,
				Description: "Fetch a single product by id.",
				Args:        graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, _ := p.Args["id"].(string)
					return orNil(findByID(gqlProducts, id)), nil
				},
			},
			"products": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(productType))),
				Description: "Search products by free text and optional tag.",
				Args: graphql.FieldConfigArgument{
					"query": &graphql.ArgumentConfig{Type: graphql.String},
					"tag":   &graphql.ArgumentConfig{Type: graphql.String},
					"limit": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 20},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					q, _ := p.Args["query"].(string)
					tag, _ := p.Args["tag"].(string)
					out := []map[string]any{}
					for _, prod := range gqlProducts {
						if q != "" && !strings.Contains(strings.ToLower(prod["name"].(string)), strings.ToLower(q)) {
							continue
						}
						if tag != "" && !hasTag(prod, tag) {
							continue
						}
						out = append(out, prod)
					}
					return out, nil
				},
			},
			"order": &graphql.Field{
				Type:        orderType,
				Description: "Fetch an order by id.",
				Args:        graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, _ := p.Args["id"].(string)
					order := buildOrder("1", []any{map[string]any{"productId": "1", "quantity": 2}})
					order["id"] = id
					return order, nil
				},
			},
		},
	})

	mutation := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"placeOrder": &graphql.Field{
				Type:        graphql.NewNonNull(orderType),
				Description: "Place a new order for a user.",
				Args:        graphql.FieldConfigArgument{"input": &graphql.ArgumentConfig{Type: graphql.NewNonNull(placeOrderInput)}},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					input, _ := p.Args["input"].(map[string]any)
					userID, _ := input["userId"].(string)
					lines, _ := input["lines"].([]any)
					return buildOrder(userID, lines), nil
				},
			},
			"cancelOrder": &graphql.Field{
				Type:        graphql.NewNonNull(orderType),
				Description: "Cancel an order that has not shipped yet.",
				Args:        graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, _ := p.Args["id"].(string)
					order := buildOrder("1", nil)
					order["id"] = id
					order["status"] = "CANCELLED"
					return order, nil
				},
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{Query: query, Mutation: mutation})
}

func hasTag(product map[string]any, tag string) bool {
	tags, _ := product["tags"].([]any)
	for _, t := range tags {
		if s, ok := t.(string); ok && strings.EqualFold(s, tag) {
			return true
		}
	}
	return false
}

// graphqlHandler serves POST /graphql in the shape the console sends:
// {"query": "...", "variables": {...}}.
func graphqlHandler(schema graphql.Schema) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"errors": []gin.H{{"message": err.Error()}}})
			return
		}
		result := graphql.Do(graphql.Params{
			Schema:         schema,
			RequestString:  body.Query,
			VariableValues: body.Variables,
		})
		// GraphQL reports field errors in the body with a 200; only a totally
		// unusable request is a transport-level failure.
		c.JSON(http.StatusOK, result)
	}
}
