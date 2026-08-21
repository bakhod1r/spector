package users

import (
	"github.com/gin-gonic/gin"

	"example.com/layered/httpx"
)

type CreateUserRequest struct {
	Email string `json:"email"`
}

type UserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount is the name half the bounded contexts in a project give this method,
// which is why resolving it by name alone attaches one context's group prefix
// to another's routes.
func (h *Handler) Mount(router gin.IRouter, guards ...gin.HandlerFunc) {
	group := router.Group("/users")
	// The handler is the last element of an append, not the last argument.
	group.POST("", append(guards, h.Create)...)
	group.GET("/:userID", append(guards, h.Get)...)
	group.POST("/:userID/verify/email", append(guards, h.Verify("email"))...)
	group.DELETE("/:userID", append(guards, h.Delete)...)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateUserRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	httpx.Created(c, UserResponse{ID: "1", Email: req.Email})
}

func (h *Handler) Get(c *gin.Context) {
	httpx.OK(c, UserResponse{ID: c.Param("userID")})
}

// Verify is a handler factory: the function's own body serves no request, and
// reading it instead of the literal documents the endpoint with nothing.
func (h *Handler) Verify(channel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateUserRequest
		if !httpx.BindJSON(c, &req) {
			return
		}
		httpx.OK(c, UserResponse{ID: channel})
	}
}

// Delete answers 204 three calls away from the framework.
func (h *Handler) Delete(c *gin.Context) {
	h.moderate(c, nil)
}

func (h *Handler) moderate(c *gin.Context, err error) { h.run(c, err) }

func (h *Handler) run(c *gin.Context, err error) {
	if err != nil {
		c.JSON(400, httpx.Envelope{})
		return
	}
	httpx.NoContent(c)
}
