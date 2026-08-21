package orders

import (
	"net/http"

	"github.com/julienschmidt/httprouter"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(r *httprouter.Router) {
	r.GET("/api/v1/orders", h.List)
	r.POST("/api/v1/orders", h.Create)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	httpx.OK(w, []OrderResp{})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	httpx.Created(w, OrderResp{ID: "1"})
}
