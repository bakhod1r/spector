package orders

import (
	"github.com/labstack/echo/v4"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(g *echo.Group) {
	g.GET("/orders", h.List)
	g.POST("/orders", h.Create)
}

func (h *Handler) List(c echo.Context) error {
	return httpx.OK(c, []OrderResp{})
}

func (h *Handler) Create(c echo.Context) error {
	return httpx.Created(c, OrderResp{ID: "1"})
}
