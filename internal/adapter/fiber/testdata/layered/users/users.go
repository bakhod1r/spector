package users

import (
	"github.com/gofiber/fiber/v2"

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

func (h *Handler) Mount(r fiber.Router, guards ...fiber.Handler) {
	r.Post("/users", append(guards, h.Create)...)
	r.Get("/users/:userID", h.Get)
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreateUserReq
	if !httpx.BindJSON(c, &req) {
		return nil
	}
	return httpx.Created(c, UserResp{ID: req.Email})
}

func (h *Handler) Get(c *fiber.Ctx) error {
	return httpx.OK(c, UserResp{ID: "1"})
}
