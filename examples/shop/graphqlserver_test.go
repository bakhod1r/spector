package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// exec runs a GraphQL document through the real handler and returns the
// decoded response.
func execGQL(t *testing.T, query string, vars map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, router(), http.MethodPost, "/graphql", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, w.Body.String())
	}
	return out
}

// data returns the data map, failing if the response carried errors.
func data(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	if errs, ok := res["errors"]; ok && errs != nil {
		t.Fatalf("query returned errors: %v", errs)
	}
	d, _ := res["data"].(map[string]any)
	if d == nil {
		t.Fatalf("no data in response: %v", res)
	}
	return d
}

func TestGraphqlSchemaBuilds(t *testing.T) {
	if _, err := newGraphqlSchema(); err != nil {
		t.Fatalf("schema does not build: %v", err)
	}
}

func TestGraphqlQueryUser(t *testing.T) {
	res := execGQL(t, `query user($id: ID!) { user(id: $id) { id name email roles } }`,
		map[string]any{"id": "1"})
	user, _ := data(t, res)["user"].(map[string]any)
	if user == nil {
		t.Fatalf("no user in %v", res)
	}
	if user["name"] != "Ada" || user["email"] != "ada@example.com" {
		t.Errorf("user = %v", user)
	}
}

// An unknown id resolves to null, which is what the nullable return type says.
func TestGraphqlQueryUserNotFound(t *testing.T) {
	res := execGQL(t, `{ user(id: "999") { id } }`, nil)
	if got := data(t, res)["user"]; got != nil {
		t.Errorf("user = %v, want null", got)
	}
}

func TestGraphqlQueryUsersRespectsLimit(t *testing.T) {
	all, _ := data(t, execGQL(t, `{ users { id } }`, nil))["users"].([]any)
	if len(all) != len(gqlUsers) {
		t.Errorf("users = %d, want %d", len(all), len(gqlUsers))
	}

	limited, _ := data(t, execGQL(t, `{ users(limit: 2) { id } }`, nil))["users"].([]any)
	if len(limited) != 2 {
		t.Errorf("limited = %d, want 2", len(limited))
	}
}

// A limit past the end clamps rather than slicing out of range.
func TestGraphqlQueryUsersLimitClamps(t *testing.T) {
	got, _ := data(t, execGQL(t, `{ users(limit: 999) { id } }`, nil))["users"].([]any)
	if len(got) != len(gqlUsers) {
		t.Errorf("users = %d, want %d", len(got), len(gqlUsers))
	}
}

func TestGraphqlQueryProduct(t *testing.T) {
	res := execGQL(t, `{ product(id: "1") { id name inStock price { amount currency } } }`, nil)
	p, _ := data(t, res)["product"].(map[string]any)
	if p == nil {
		t.Fatalf("no product in %v", res)
	}
	if p["name"] != "Widget" || p["inStock"] != true {
		t.Errorf("product = %v", p)
	}
	price, _ := p["price"].(map[string]any)
	if price["currency"] != "USD" {
		t.Errorf("price = %v", price)
	}
}

func TestGraphqlQueryProductsFilters(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"all", `{ products { id } }`, 2},
		{"by name", `{ products(query: "widget") { id } }`, 1},
		{"by name, case-insensitive", `{ products(query: "WIDGET") { id } }`, 1},
		{"by tag", `{ products(tag: "new") { id } }`, 1},
		{"tag shared by both", `{ products(tag: "tools") { id } }`, 2},
		{"no match", `{ products(query: "nothing") { id } }`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := data(t, execGQL(t, tc.query, nil))["products"].([]any)
			if len(got) != tc.want {
				t.Errorf("products = %d, want %d", len(got), tc.want)
			}
		})
	}
}

func TestGraphqlQueryOrder(t *testing.T) {
	res := execGQL(t, `{ order(id: "77") { id status total { amount } lines { quantity product { name } } } }`, nil)
	o, _ := data(t, res)["order"].(map[string]any)
	if o == nil {
		t.Fatalf("no order in %v", res)
	}
	if o["id"] != "77" {
		t.Errorf("id = %v, want the requested id", o["id"])
	}
	// The enum must come back as its name, not an integer.
	if o["status"] != "PENDING" {
		t.Errorf("status = %v, want PENDING", o["status"])
	}
}

func TestGraphqlMutationPlaceOrder(t *testing.T) {
	res := execGQL(t,
		`mutation placeOrder($input: PlaceOrderInput!) {
			placeOrder(input: $input) { id status total { amount currency } lines { quantity } }
		}`,
		map[string]any{"input": map[string]any{
			"userId": "1",
			"lines":  []any{map[string]any{"productId": "1", "quantity": 3}},
		}})

	o, _ := data(t, res)["placeOrder"].(map[string]any)
	if o == nil {
		t.Fatalf("no order in %v", res)
	}
	total, _ := o["total"].(map[string]any)
	// 9.99 x 3, computed by the resolver rather than echoed back.
	if amount, _ := total["amount"].(float64); amount < 29.96 || amount > 29.98 {
		t.Errorf("total = %v, want 29.97", total["amount"])
	}
	lines, _ := o["lines"].([]any)
	if len(lines) != 1 {
		t.Errorf("lines = %d, want 1", len(lines))
	}
}

// Lines naming a product that does not exist are dropped rather than
// producing a null entry in a non-null list.
func TestGraphqlMutationPlaceOrderSkipsUnknownProducts(t *testing.T) {
	res := execGQL(t,
		`mutation placeOrder($input: PlaceOrderInput!) { placeOrder(input: $input) { lines { quantity } total { amount } } }`,
		map[string]any{"input": map[string]any{
			"userId": "1",
			"lines":  []any{map[string]any{"productId": "999", "quantity": 2}},
		}})
	o, _ := data(t, res)["placeOrder"].(map[string]any)
	lines, _ := o["lines"].([]any)
	if len(lines) != 0 {
		t.Errorf("lines = %d, want 0", len(lines))
	}
}

// An unknown user falls back to the first one rather than failing the order.
func TestGraphqlMutationPlaceOrderUnknownUser(t *testing.T) {
	res := execGQL(t,
		`mutation placeOrder($input: PlaceOrderInput!) { placeOrder(input: $input) { user { id } } }`,
		map[string]any{"input": map[string]any{"userId": "999", "lines": []any{}}})
	o, _ := data(t, res)["placeOrder"].(map[string]any)
	user, _ := o["user"].(map[string]any)
	if user["id"] != "1" {
		t.Errorf("user = %v, want the fallback", user)
	}
}

func TestGraphqlMutationCancelOrder(t *testing.T) {
	res := execGQL(t, `mutation { cancelOrder(id: "42") { id status } }`, nil)
	o, _ := data(t, res)["cancelOrder"].(map[string]any)
	if o["id"] != "42" || o["status"] != "CANCELLED" {
		t.Errorf("order = %v", o)
	}
}

// A field that is not in the schema is a GraphQL error with HTTP 200, which is
// what the console distinguishes from a transport failure.
func TestGraphqlUnknownFieldIsErrorNot500(t *testing.T) {
	w := do(t, router(), http.MethodPost, "/graphql", `{"query":"{ nope }"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["errors"] == nil {
		t.Errorf("body = %s, want an errors array", w.Body.String())
	}
}

// A body that is not JSON at all cannot be executed, so it is a 400.
func TestGraphqlMalformedBodyIs400(t *testing.T) {
	w := do(t, router(), http.MethodPost, "/graphql", "{not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "errors") {
		t.Errorf("body = %s, want an errors array", w.Body.String())
	}
}

// ---- helpers ----

func TestFindByID(t *testing.T) {
	if got := findByID(gqlUsers, "2"); got == nil || got["name"] != "Alan" {
		t.Errorf("= %v, want Alan", got)
	}
	if got := findByID(gqlUsers, "nope"); got != nil {
		t.Errorf("= %v, want nil", got)
	}
	if got := findByID(nil, "1"); got != nil {
		t.Errorf("= %v, want nil for an empty set", got)
	}
}

func TestHasTag(t *testing.T) {
	p := gqlProducts[1] // tags: tools, new
	if !hasTag(p, "new") {
		t.Error("hasTag missed an exact match")
	}
	if !hasTag(p, "NEW") {
		t.Error("hasTag should be case-insensitive")
	}
	if hasTag(p, "absent") {
		t.Error("hasTag matched a tag that is not there")
	}
	if hasTag(map[string]any{}, "x") {
		t.Error("hasTag matched on a product with no tags")
	}
}

func TestBuildOrderTotalsLines(t *testing.T) {
	o := buildOrder("1", []any{
		map[string]any{"productId": "1", "quantity": 2}, // 9.99 x 2
		map[string]any{"productId": "2", "quantity": 1}, // 24.50
	})
	total := o["total"].(map[string]any)["amount"].(float64)
	if total < 44.47 || total > 44.49 {
		t.Errorf("total = %v, want 44.48", total)
	}
}

// Entries that are not line objects are ignored rather than panicking.
func TestBuildOrderIgnoresMalformedLines(t *testing.T) {
	o := buildOrder("1", []any{"not a line", 42, nil})
	if lines := o["lines"].([]any); len(lines) != 0 {
		t.Errorf("lines = %v, want none", lines)
	}
}
