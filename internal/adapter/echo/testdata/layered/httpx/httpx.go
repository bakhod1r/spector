// Package httpx is the helper package an echo project grows: bind, then answer
// through one envelope.
package httpx

import "github.com/labstack/echo/v4"

type Envelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func BindJSON[T any](c echo.Context, dto *T) bool {
	if err := c.Bind(dto); err != nil {
		c.JSON(422, Envelope{Error: "invalid"})
		return false
	}
	return true
}

func OK(c echo.Context, data any) error      { return c.JSON(200, Envelope{Data: data}) }
func Created(c echo.Context, data any) error { return c.JSON(201, Envelope{Data: data}) }
