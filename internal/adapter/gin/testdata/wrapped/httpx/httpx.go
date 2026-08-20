// Package httpx is the wrapper every project grows: the handlers call it, not
// gin. Its names are deliberately the framework's own — BindJSON is spelled
// exactly like gin's method — because that is what real helper packages look
// like, and matching the name instead of stepping into the body loses both the
// request type and the 400 written here.
package httpx

import "github.com/gin-gonic/gin"

func BindJSON(c *gin.Context, out any) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func Created(c *gin.Context, body any) { c.JSON(201, body) }
