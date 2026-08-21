package orders

import (
	"net/http"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/orders", h.List)
	mux.HandleFunc("POST /api/v1/orders", h.Create)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, []OrderResp{})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	httpx.Created(w, OrderResp{ID: "1"})
}
