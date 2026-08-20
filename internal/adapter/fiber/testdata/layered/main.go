package main

import (
	"github.com/gofiber/fiber/v2"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	app := fiber.New()
	api := app.Group("/api/v1")
	register(api)
	app.Listen(":8080")
}

func register(api fiber.Router) {
	users.NewHandler().Mount(api)
	orders.NewHandler().Mount(api)
}
