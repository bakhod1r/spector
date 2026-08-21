package orders

import (
	"net/http"

	"github.com/uptrace/bunrouter"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(g *bunrouter.Group) {
	g.GET("/orders", h.List)
	g.POST("/orders", h.Create)
}

func (h *Handler) List(w http.ResponseWriter, r bunrouter.Request) error {
	httpx.OK(w, []OrderResp{})
	return nil
}

func (h *Handler) Create(w http.ResponseWriter, r bunrouter.Request) error {
	httpx.Created(w, OrderResp{ID: "1"})
	return nil
}
