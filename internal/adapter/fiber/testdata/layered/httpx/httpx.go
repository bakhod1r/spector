// Package httpx is the helper package a fiber project grows: bind, then answer
// through one envelope.
package httpx

import "github.com/gofiber/fiber/v2"

type Envelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func BindJSON[T any](c *fiber.Ctx, dto *T) bool {
	if err := c.BodyParser(dto); err != nil {
		c.Status(422).JSON(Envelope{Error: "invalid"})
		return false
	}
	return true
}

func OK(c *fiber.Ctx, data any) error      { return c.Status(200).JSON(Envelope{Data: data}) }
func Created(c *fiber.Ctx, data any) error { return c.Status(201).JSON(Envelope{Data: data}) }
