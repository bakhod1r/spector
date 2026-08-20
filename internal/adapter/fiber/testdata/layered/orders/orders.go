package orders

import (
	"github.com/gofiber/fiber/v2"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(r fiber.Router) {
	r.Get("/orders", h.List)
	r.Post("/orders", h.Create)
}

func (h *Handler) List(c *fiber.Ctx) error {
	return httpx.OK(c, []OrderResp{})
}

func (h *Handler) Create(c *fiber.Ctx) error {
	return httpx.Created(c, OrderResp{ID: "1"})
}
