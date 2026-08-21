package users

import (
	"net/http"

	"github.com/gorilla/mux"

	"example.com/layered/httpx"
)

type CreateUserReq struct {
	Email string `json:"email"`
}

type UserResp struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Mount(r *mux.Router) {
	r.HandleFunc("/users", h.Create).Methods("POST")
	r.HandleFunc("/users/{userID}", h.Get).Methods("GET")
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateUserReq
	if !httpx.BindJSON(w, r, &req) {
		return
	}
	httpx.Created(w, UserResp{ID: req.Email})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, UserResp{ID: "1"})
}
