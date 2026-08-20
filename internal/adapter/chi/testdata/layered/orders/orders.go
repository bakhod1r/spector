package orders

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/orders", h.List)
	r.Post("/orders", h.Create)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, []OrderResp{})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	httpx.Created(w, OrderResp{ID: "1"})
}
