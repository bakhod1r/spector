// Package httpx is the response envelope every layered project grows. Every
// endpoint answers through it, so read literally every endpoint returns the
// same type and the document names no payload at all.
package httpx

import "github.com/gin-gonic/gin"

type Envelope struct {
	Data  any   `json:"data,omitempty"`
	Meta  *Meta `json:"meta,omitempty"`
	Error *Fail `json:"error,omitempty"`
}

type Meta struct {
	Total int `json:"total"`
}

type Fail struct {
	Message string `json:"message"`
}

func BindJSON[T any](c *gin.Context, dto *T) bool {
	if err := c.ShouldBindJSON(dto); err != nil {
		c.JSON(422, Envelope{Error: &Fail{Message: "invalid"}})
		return false
	}
	return true
}

func OK(c *gin.Context, data any)      { c.JSON(200, Envelope{Data: data}) }
func Created(c *gin.Context, data any) { c.JSON(201, Envelope{Data: data}) }
func NoContent(c *gin.Context)         { c.Status(204) }
