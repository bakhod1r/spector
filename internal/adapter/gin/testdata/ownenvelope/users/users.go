package users

import "github.com/gin-gonic/gin"

// Envelope is this context's own, declared under the same name the orders
// context uses for its own copy.
type Envelope struct {
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

type UserResponse struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Mount(router gin.IRouter) {
	group := router.Group("/users")
	group.POST("", h.Create)
}

func (h *Handler) Create(c *gin.Context) {
	OK(c, UserResponse{ID: "1"})
}

func OK(c *gin.Context, data any) { c.JSON(200, Envelope{Data: data}) }
