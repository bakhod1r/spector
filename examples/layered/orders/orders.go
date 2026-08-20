// Package orders is the second bounded context. It declares Handler, Mount,
// NewHandler, Create, List and Get — the same six names the users context
// declares, which is exactly what makes name-based resolution wrong.
package orders

import (
	"github.com/gin-gonic/gin"

	"github.com/bakhod1r/spector/examples/layered/httpx"
)

type CreateOrderRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Total  int64  `json:"total" binding:"required,gt=0"`
}

type OrderResponse struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Total  int64  `json:"total"`
	Status string `json:"status"`
}

type Handler struct {
	orders map[string]OrderResponse
}

func NewHandler() *Handler {
	return &Handler{orders: map[string]OrderResponse{
		"1": {ID: "1", UserID: "1", Total: 4200, Status: "paid"},
	}}
}

// Mount registers this context's routes. Same name as the users context's
// Mount, and it takes a different set of parameters — so a scan that matched
// on the name alone handed one context the other's group prefix.
func (h *Handler) Mount(router gin.IRouter) {
	group := router.Group("/orders")

	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:orderID", h.Get)
}

// Create places an order.
func (h *Handler) Create(c *gin.Context) {
	var req CreateOrderRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	o := OrderResponse{ID: "2", UserID: req.UserID, Total: req.Total, Status: "pending"}
	h.orders[o.ID] = o
	httpx.Created(c, o)
}

// List returns every order.
func (h *Handler) List(c *gin.Context) {
	out := make([]OrderResponse, 0, len(h.orders))
	for _, o := range h.orders {
		out = append(out, o)
	}
	httpx.OK(c, out)
}

// Get returns one order.
func (h *Handler) Get(c *gin.Context) {
	o, ok := h.orders[c.Param("orderID")]
	if !ok {
		c.JSON(404, httpx.Envelope{Error: &httpx.Fail{Message: "not found"}})
		return
	}
	httpx.OK(c, o)
}
