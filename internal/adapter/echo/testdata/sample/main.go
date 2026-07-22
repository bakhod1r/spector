package sample

import (
	"net/http"

	"github.com/labstack/echo/v4"
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
func listUsers(c echo.Context) error {
	q := c.QueryParam("q")
	limit := c.QueryParam("limit")
	_, _ = q, limit
	out := []User{}
	return c.JSON(http.StatusOK, out)
}

func getUser(c echo.Context) error {
	var u User
	return c.JSON(http.StatusOK, u)
}

func createUser(c echo.Context) error {
	var req CreateUserReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, User{})
}

func deleteUser(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

func health(c echo.Context) error {
	token := c.Request().Header.Get("X-Token")
	_ = token
	return c.JSON(http.StatusOK, echo.Map{"ok": true})
}

func catchAll(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func requestID(next echo.HandlerFunc) echo.HandlerFunc { return next }

// tenantGuard is named after nothing in particular: what it demands is only
// visible in its body.
func tenantGuard(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get("X-Tenant-Key") == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "no tenant")
		}
		return next(c)
	}
}

func adminOnly(next echo.HandlerFunc) echo.HandlerFunc { return next }

func Register(e *echo.Echo) {
	e.Use(requestID)

	api := e.Group("/api")
	v1 := api.Group("/v1", tenantGuard)

	v1.GET("/users", listUsers)
	v1.GET("/users/:id", getUser)
	v1.POST("/users", createUser)
	v1.DELETE("/users/:id", deleteUser, adminOnly)

	e.GET("/health", health)
	e.Any("/proxy", catchAll)
	e.Match([]string{"GET", "POST"}, "/dual", catchAll)
	e.GET("/files/*", catchAll)
}
