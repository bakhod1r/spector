package edge

import "net/http"

type Ctx struct{}

func listUsers(c *Ctx) error  { return nil }
func getUser(c *Ctx) error    { return nil }
func createUser(c *Ctx) error { return nil }
func anything(c *Ctx) error   { return nil }
func added(c *Ctx) error      { return nil }
func addedSel(c *Ctx) error   { return nil }
func nested(c *Ctx) error     { return nil }
func authMW(c *Ctx) error     { return nil }

type app struct{}

func (a *app) Get(path string, h ...func(*Ctx) error)              {}
func (a *app) Post(path string, h ...func(*Ctx) error)             {}
func (a *app) All(path string, h ...func(*Ctx) error)              {}
func (a *app) Add(m, path string, h ...func(*Ctx) error)           {}
func (a *app) Group(prefix string, h ...func(*Ctx) error) *app     { return a }
func (a *app) Use(h ...func(*Ctx) error)                           {}

func newApp() *app { return &app{} }

func routes() {
	app := newApp()

	app.Get("/users", listUsers)
	app.Get("/users/:id", getUser)
	app.Get("/files/*", anything)
	app.Get("/opt/:id?", getUser)
	// Inline middleware before the handler.
	app.Post("/users", authMW, createUser)
	// Non-literal path is skipped.
	dyn := "/dyn"
	app.Get(dyn, listUsers)
	// Fewer than two args is skipped.
	app.Get("/half")

	// All registers the common methods.
	app.All("/any", anything)
	app.All(dyn, anything)

	// Add with a string method, a selector method, and an unknown method.
	app.Add("GET", "/added", added)
	app.Add(http.MethodPost, "/added-sel", addedSel)
	app.Add("TRACE", "/nope", added)
	m := "GET"
	app.Add(m, "/var-method", added)
	app.Add("GET", dyn, added)

	// Groups nest; the self-assigned group must not loop.
	api := app.Group("/api")
	v1 := api.Group("/v1")
	v1.Get("/nested", nested)
	v1 = v1.Group("/again")

	// Group with a non-literal prefix, a non-ident receiver, and a
	// multi-assign are all skipped.
	bad := app.Group(dyn)
	bad.Get("/x", listUsers)
	newApp().Group("/call").Get("/y", listUsers)
	a, b := 1, 2
	_, _ = a, b
	var holder struct{ g *app }
	holder.g = app.Group("/held")

	// Plain calls that are not registrations.
	println("not a route")
}
