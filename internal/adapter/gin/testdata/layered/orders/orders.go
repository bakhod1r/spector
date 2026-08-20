package orders

import (
	"github.com/gin-gonic/gin"

	"example.com/layered/httpx"
)

type OrderResponse struct {
	ID string `json:"id"`
}

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Mount collides by name with the users context's Mount, and Create collides
// with its Create.
func (h *Handler) Mount(router gin.IRouter) {
	group := router.Group("/orders")
	group.GET("", h.List)
	group.POST("", h.Create)
}

func (h *Handler) List(c *gin.Context) {
	httpx.OK(c, []OrderResponse{})
}

func (h *Handler) Create(c *gin.Context) {
	httpx.Created(c, OrderResponse{ID: "1"})
}
