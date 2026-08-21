// Package users is one bounded context. Its handler type is called Handler,
// its registration method is called Mount, and it declares Create and Get —
// every one of those names is declared again by the orders context next door.
// Resolving a route's handler by bare name picks whichever sorted first, so
// one context ends up documented from the other's body.
package users

import (
	"github.com/gin-gonic/gin"

	"github.com/bakhod1r/spector/examples/layered/httpx"
)

type CreateUserRequest struct {
	Email string `json:"email" binding:"required,email"`
	Name  string `json:"name" binding:"required,min=2,max=64"`
}

type UserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type Handler struct {
	users map[string]UserResponse
}

func NewHandler() *Handler {
	return &Handler{users: map[string]UserResponse{
		"1": {ID: "1", Email: "ada@example.com", Name: "Ada"},
	}}
}

// Mount registers this context's routes on whatever router it is handed. The
// router carries a prefix the caller composed; nothing here knows what it is.
//
// guards is the middleware the caller wants in front of these routes, which is
// why the handler is the last element of an append rather than the last
// argument of the call.
func (h *Handler) Mount(router gin.IRouter, guards ...gin.HandlerFunc) {
	group := router.Group("/users")

	group.POST("", append(guards, h.Create)...)
	group.GET("", append(guards, h.List)...)
	group.GET("/:userID", append(guards, h.Get)...)
	group.DELETE("/:userID", append(guards, h.Delete)...)

	// A handler factory: one implementation, parameterised per route. The
	// method's own body serves no request — the closure it returns does.
	group.POST("/:userID/verify/email", append(guards, h.Verify("email"))...)
	group.POST("/:userID/verify/phone", append(guards, h.Verify("phone"))...)
}

// Create registers a user.
func (h *Handler) Create(c *gin.Context) {
	var req CreateUserRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	u := UserResponse{ID: "2", Email: req.Email, Name: req.Name}
	h.users[u.ID] = u
	httpx.Created(c, u)
}

// List returns every user, with paging metadata.
func (h *Handler) List(c *gin.Context) {
	out := make([]UserResponse, 0, len(h.users))
	for _, u := range h.users {
		out = append(out, u)
	}
	httpx.Page(c, out, httpx.Meta{Total: len(out), Limit: 20})
}

// Get returns one user.
func (h *Handler) Get(c *gin.Context) {
	u, ok := h.users[c.Param("userID")]
	if !ok {
		c.JSON(404, httpx.Envelope{Error: &httpx.Fail{Message: "not found"}})
		return
	}
	httpx.OK(c, u)
}

// Delete removes a user. The 204 it answers with is written three calls out of
// this body — through moderate, then run, then the helper package.
func (h *Handler) Delete(c *gin.Context) {
	h.moderate(c, func(id string) error {
		delete(h.users, id)
		return nil
	})
}

// Verify confirms a contact. It is a factory: the channel is fixed when the
// route is registered, and the returned closure is the handler.
func (h *Handler) Verify(channel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req VerifyRequest
		if !httpx.BindJSON(c, &req) {
			return
		}
		httpx.OK(c, VerifyResponse{Channel: channel, Verified: req.Code == "000000"})
	}
}

type VerifyRequest struct {
	Code string `json:"code" binding:"required,len=6"`
}

type VerifyResponse struct {
	Channel  string `json:"channel"`
	Verified bool   `json:"verified"`
}

func (h *Handler) moderate(c *gin.Context, fn func(id string) error) {
	h.run(c, fn(c.Param("userID")))
}

func (h *Handler) run(c *gin.Context, err error) {
	if err != nil {
		c.JSON(400, httpx.Envelope{Error: &httpx.Fail{Message: err.Error()}})
		return
	}
	httpx.NoContent(c)
}
