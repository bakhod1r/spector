// Command layered is a service shaped the way real Go services are shaped once
// they outgrow a single file, and it exists because that shape used to
// document as almost nothing.
//
// Four things here are ordinary, and each one used to cost the document:
//
//   - Two bounded contexts declare the same names — Handler, Mount, NewHandler,
//     Create, List, Get. Resolving a route's handler by bare name picks
//     whichever declaration sorted first, so one context is documented from the
//     other's body: the wrong summary, the wrong source link, no types.
//
//   - Handlers reach gin only through a helper package. A scan that stops at
//     the handler body finds no request type and no response type.
//
//   - Every response is wrapped in one envelope whose payload field is `any`,
//     so the whole API appears to return the same shape.
//
//   - The group prefix reaches the registration through a function parameter
//     and another package's method, so routes land at their bare paths.
//
// Run it and open http://localhost:8080/docs: every endpoint carries its
// request schema, its status codes and an EnvelopeOf… response naming the
// payload it actually returns.
package main

import (
	"github.com/gin-gonic/gin"

	"github.com/bakhod1r/spector"
	"github.com/bakhod1r/spector/examples/layered/orders"
	"github.com/bakhod1r/spector/examples/layered/users"
	"github.com/bakhod1r/spector/mount"
)

func main() {
	router().Run(":8080")
}

// router builds the engine and registers every route. It is separate from main
// so tests can drive the API without binding a port.
func router() *gin.Engine {
	r := gin.New()

	api := r.Group("/api/v1")
	register(api)

	mount.Gin(r, spector.Config{
		Dir:     ".",
		Title:   "Layered API",
		Version: "1.0.0",
	})
	return r
}

// register takes the group as a parameter, opens a middleware-only sub-group
// on it, and hands that to each context. Three hops — a parameter, a bare
// Group(""), and another package's method — and the prefix has to survive all
// of them or every route below documents at its bare path.
func register(api gin.IRouter) {
	private := api.Group("", requestID())

	users.NewHandler().Mount(private, auth())
	orders.NewHandler().Mount(private)
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Request-Id", "req-1")
		c.Next()
	}
}

func auth() gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}
