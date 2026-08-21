package orders

import (
	"net/http"

	"github.com/gorilla/mux"

	"example.com/layered/httpx"
)

type OrderResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount and Create collide by name with the users context.
func (h *Handler) Mount(r *mux.Router) {
	r.HandleFunc("/orders", h.List).Methods("GET")
	r.HandleFunc("/orders", h.Create).Methods("POST")
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, []OrderResp{})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	httpx.Created(w, OrderResp{ID: "1"})
}
