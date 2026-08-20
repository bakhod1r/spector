package users

import (
	"net/http"

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

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users", h.Create)
	mux.HandleFunc("GET /api/v1/users/{userID}", h.Get)
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
