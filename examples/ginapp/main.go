package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/user/specter"
	"github.com/user/specter/mount"
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

var users = map[int]User{
	1: {ID: 1, Name: "Ada", Email: "ada@example.com"},
	2: {ID: 2, Name: "Alan", Email: "alan@example.com"},
}

func listUsers(c *gin.Context) {
	q := c.Query("q")
	limit := c.DefaultQuery("limit", "10")
	_ = limit
	out := []User{}
	for _, u := range users {
		if q != "" && !strings.Contains(strings.ToLower(u.Name), strings.ToLower(q)) {
			continue
		}
		out = append(out, u)
	}
	c.JSON(http.StatusOK, out)
}

func getUser(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var u User
	u, ok := users[id]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, u)
}

func createUser(c *gin.Context) {
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u := User{ID: len(users) + 1, Name: req.Name, Email: req.Email}
	users[u.ID] = u
	c.JSON(http.StatusCreated, u)
}

func main() {
	router().Run(":8080")
}

// router builds the engine and registers every route. It is separate from
// main so tests can drive the API without binding a port.
func router() *gin.Engine {
	r := gin.Default()

	v1 := r.Group("/api/v1")
	v1.GET("/users", listUsers)
	v1.GET("/users/:id", getUser)
	v1.POST("/users", createUser)

	mount.Gin(r, specter.Config{
		Dir:     ".",
		Title:   "Users API",
		Version: "1.0.0",
	})

	return r
}
