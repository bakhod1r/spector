package sample

import "github.com/gin-gonic/gin"

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type CreateUserReq struct {
	Name string `json:"name"`
}

func Register(r *gin.Engine) {
	v1 := r.Group("/api/v1")
	v1.GET("/users", listUsers)
	v1.GET("/users/:id", getUser)
	v1.POST("/users", createUser)
}

// listUsers returns every user.
// specter:tags users
func listUsers(c *gin.Context) {
	_ = c.Query("limit")
	_ = c.GetHeader("X-Tenant")
	out := []User{}
	c.JSON(200, out)
}

func getUser(c *gin.Context) {
	var u User
	c.JSON(200, u)
}

func createUser(c *gin.Context) {
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return
	}
	c.JSON(201, &User{})
}
