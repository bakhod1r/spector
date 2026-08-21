package main

import (
	"net/http"

	"github.com/uptrace/bunrouter"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	r := bunrouter.New()
	// The group exists only inside the closure, so the contexts registered
	// from it have to be followed with the prefix carried along.
	r.WithGroup("/api/v1", func(g *bunrouter.Group) {
		users.NewHandler().Mount(g)
		orders.NewHandler().Mount(g)
	})
	http.ListenAndServe(":8080", r)
}
