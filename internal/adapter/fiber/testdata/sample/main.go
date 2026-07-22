package sample

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type CreateUserReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// listUsers returns every user.
//
// Supports free-text search and a page size.
// specter:tags users
func listUsers(c *fiber.Ctx) error {
	q := c.Query("q")
	limit := c.Query("limit")
	_, _ = q, limit
	out := []User{}
	return c.JSON(out)
}

func getUser(c *fiber.Ctx) error {
	var u User
	return c.JSON(u)
}

func createUser(c *fiber.Ctx) error {
	var req CreateUserReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(http.StatusCreated).JSON(User{})
}

func deleteUser(c *fiber.Ctx) error {
	return c.SendStatus(http.StatusNoContent)
}

func health(c *fiber.Ctx) error {
	token := c.GetReqHeaders()["X-Token"]
	_ = token
	return c.JSON(fiber.Map{"ok": true})
}

func catchAll(c *fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func requestID(c *fiber.Ctx) error { return c.Next() }

// tenantGuard is named after nothing in particular: what it demands is only
// visible in its body.
func tenantGuard(c *fiber.Ctx) error {
	if c.Get("X-Tenant-Key") == "" {
		return c.SendStatus(http.StatusUnauthorized)
	}
	return c.Next()
}

func adminOnly(c *fiber.Ctx) error { return c.Next() }

func Register(app *fiber.App) {
	app.Use(requestID)

	api := app.Group("/api")
	v1 := api.Group("/v1", tenantGuard)

	v1.Get("/users", listUsers)
	v1.Get("/users/:id", getUser)
	v1.Post("/users", createUser)
	v1.Delete("/users/:id", adminOnly, deleteUser)

	app.Get("/health", health)
	app.All("/proxy", catchAll)
	app.Add("GET", "/manual", catchAll)
	app.Get("/files/*", catchAll)
	app.Get("/opt/:name?", catchAll)
}
