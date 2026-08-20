package main

import (
	"github.com/gin-gonic/gin"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	engine := gin.New()
	api := engine.Group("/api/v1")
	register(api)
	engine.Run()
}

// register receives the group as a parameter and opens a bare sub-group on it,
// then hands that to two other packages' methods. The prefix has to survive
// all three hops or the routes document at their bare paths.
func register(api gin.IRouter) {
	private := api.Group("", auth())

	users.NewHandler().Mount(private, auth())
	orders.NewHandler().Mount(private)
}

func auth() gin.HandlerFunc { return func(c *gin.Context) {} }
