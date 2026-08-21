// Package main is a tree where two bounded contexts each declare their own
// response envelope instead of sharing one from a helper package. Copying the
// envelope per context is at least as common as importing it, and the two
// copies normally have the same shape.
package main

import (
	"github.com/gin-gonic/gin"

	"example.com/ownenvelope/orders"
	"example.com/ownenvelope/users"
)

func main() {
	engine := gin.New()
	api := engine.Group("/api/v1")
	users.NewHandler().Mount(api)
	orders.NewHandler().Mount(api)
	engine.Run()
}
