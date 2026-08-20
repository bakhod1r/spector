package users

import (
	"github.com/labstack/echo/v4"

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

func (h *Handler) Mount(g *echo.Group, guards ...echo.MiddlewareFunc) {
	g.POST("/users", h.Create, guards...)
	g.GET("/users/:userID", h.Get)
}

func (h *Handler) Create(c echo.Context) error {
	var req CreateUserReq
	if !httpx.BindJSON(c, &req) {
		return nil
	}
	return httpx.Created(c, UserResp{ID: req.Email})
}

func (h *Handler) Get(c echo.Context) error {
	return httpx.OK(c, UserResp{ID: "1"})
}
