package users

import (
	"net/http"

	"github.com/uptrace/bunrouter"

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

func (h *Handler) Mount(g *bunrouter.Group) {
	g.POST("/users", h.Create)
	g.GET("/users/:userID", h.Get)
}

func (h *Handler) Create(w http.ResponseWriter, r bunrouter.Request) error {
	var req CreateUserReq
	if !httpx.BindJSON(w, r.Request, &req) {
		return nil
	}
	httpx.Created(w, UserResp{ID: req.Email})
	return nil
}

func (h *Handler) Get(w http.ResponseWriter, r bunrouter.Request) error {
	httpx.OK(w, UserResp{ID: "1"})
	return nil
}
