package main

import (
	"github.com/labstack/echo/v4"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	e := echo.New()
	api := e.Group("/api/v1")
	register(api)
	e.Start(":8080")
}

// register receives the group as a parameter and hands it to two packages'
// methods; the prefix has to survive both hops.
func register(api *echo.Group) {
	users.NewHandler().Mount(api)
	orders.NewHandler().Mount(api)
}
