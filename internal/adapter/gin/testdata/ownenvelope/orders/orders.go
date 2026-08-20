package orders

import "github.com/gin-gonic/gin"

// Envelope is this context's own copy, same shape as the users context's.
type Envelope struct {
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

type OrderResponse struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Mount(router gin.IRouter) {
	group := router.Group("/orders")
	group.POST("", h.Create)
}

func (h *Handler) Create(c *gin.Context) {
	OK(c, OrderResponse{ID: "1"})
}

func OK(c *gin.Context, data any) { c.JSON(200, Envelope{Data: data}) }
