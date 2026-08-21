package main

import "github.com/gin-gonic/gin"

type SignupRequest struct {
	Email string `json:"email"`
}

type SignupResponse struct {
	UserID string `json:"user_id"`
}

type VerifyRequest struct {
	Phone string `json:"phone"`
}

type VerifyResponse struct {
	Sent bool `json:"sent"`
}

func main() {
	engine := gin.New()
	signup := engine.Group("/signup")
	// The chain helper is the project's own, so it is not append or Concat: the
	// registration's last argument is a call to it, and the handler is that
	// call's last argument.
	signup.POST("", chain(auth(), createSignup)...)
	signup.POST("/verify/request", chain(auth(), requestVerification)...)
	engine.Run()
}

// chain builds one route's handler slice without appending into the caller's,
// so a shared guard slice is never aliased between routes.
func chain(guard gin.HandlerFunc, h gin.HandlerFunc) []gin.HandlerFunc {
	out := make([]gin.HandlerFunc, 0, 2)
	return append(append(out, guard), h)
}

func auth() gin.HandlerFunc { return func(c *gin.Context) {} }

// createSignup registers a new account.
func createSignup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, SignupResponse{UserID: "1"})
}

// requestVerification sends a verification code.
func requestVerification(c *gin.Context) {
	var req VerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, VerifyResponse{Sent: true})
}
