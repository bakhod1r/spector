package app

import "github.com/gin-gonic/gin"

type Req struct {
	Name string `json:"name"`
}

func listUsers(c *gin.Context) { c.JSON(200, []string{}) }

func getUser(c *gin.Context) { c.JSON(200, "u") }

// userByName is shadowed: /users/{id} is registered before /users/me below.
func currentUser(c *gin.Context) { c.JSON(200, "me") }

// deleteUser is written but never registered. This is the orphan.
func deleteUser(c *gin.Context) { c.Status(204) }

// helper is not a handler and must not be reported.
func helper(s string) string { return s }

// alsoNotAHandler takes a context but returns something, so it is not a gin
// handler either.
func alsoNotAHandler(c *gin.Context) error { return nil }

func router() *gin.Engine {
	r := gin.New()
	r.GET("/users", listUsers)
	r.GET("/users/:id", getUser)
	r.GET("/users/me", currentUser)
	r.GET("/users", listUsers)
	return r
}
