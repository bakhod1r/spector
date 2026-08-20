// Package httpx is the helper package a Go service grows once it has more than
// a handful of endpoints: one place that binds a request body and one place
// that writes a response, so no handler ever touches the framework directly.
//
// It is also the shape that used to cost a project its whole document. A
// scanner that stops at the handler body sees a call to BindJSON and a call to
// OK and nothing else — no request type, no response type, no schema. Spector
// reads through both, and reads the payload out of the envelope OK builds.
package httpx

import (
	"github.com/gin-gonic/gin"
)

// Envelope is the single response shape every endpoint answers in. Data is
// `any`, so the envelope alone says nothing about what an endpoint returns —
// the payload type has to come from the call site.
type Envelope struct {
	Data  any   `json:"data,omitempty"`
	Meta  *Meta `json:"meta,omitempty"`
	Error *Fail `json:"error,omitempty"`
}

type Meta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type Fail struct {
	Message string `json:"message"`
}

// BindJSON decodes the body and, on failure, writes the error itself so the
// handler can simply return. The type parameter is what real helper packages
// use; the request type is still the caller's concrete struct.
func BindJSON[T any](c *gin.Context, dto *T) bool {
	if err := c.ShouldBindJSON(dto); err != nil {
		c.JSON(422, Envelope{Error: &Fail{Message: err.Error()}})
		return false
	}
	return true
}

func OK(c *gin.Context, data any) { c.JSON(200, Envelope{Data: data}) }

func Created(c *gin.Context, data any) { c.JSON(201, Envelope{Data: data}) }

func Page(c *gin.Context, data any, meta Meta) {
	c.JSON(200, Envelope{Data: data, Meta: &meta})
}

func NoContent(c *gin.Context) { c.Status(204) }
