package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func router() *gin.Engine {
	r := gin.New()

	// Registered before any route: applies to everything below.
	r.Use(Logger())
	r.Use(Recovery())

	// Public: registered before the auth middleware exists.
	r.GET("/health", health)
	r.POST("/login", login)

	// A group created before the auth Use call must not inherit it, however
	// far below the group its own routes are registered.
	pub := r.Group("/public")

	// Everything after this line is authenticated.
	r.Use(AuthRequired())
	r.GET("/profile", profile)

	// A group inherits what was in effect when it was created, and adds its own.
	admin := r.Group("/admin", RequireAPIKey())
	admin.GET("/users", listUsers)

	// A nested group inherits both levels.
	super := admin.Group("/super")
	super.DELETE("/users/:id", deleteUser)

	// A project's own guard, registered the way such things usually are.
	sign := &SignMiddleware{}
	signed := r.Group("/v1", sign.Handler())
	signed.GET("/orders", listOrders)

	// Route-level middleware applies to this route alone.
	r.POST("/upload", RateLimit(10), upload)

	pub.GET("/docs", publicDocs)

	return r
}

func health(c *gin.Context)     {}
func login(c *gin.Context)      {}
func profile(c *gin.Context)    {}
func listUsers(c *gin.Context)  {}
func deleteUser(c *gin.Context) {}
func upload(c *gin.Context)     {}
func publicDocs(c *gin.Context) {}

func Logger() gin.HandlerFunc         { return func(c *gin.Context) {} }
func Recovery() gin.HandlerFunc       { return func(c *gin.Context) {} }
func AuthRequired() gin.HandlerFunc   { return func(c *gin.Context) {} }
func RequireAPIKey() gin.HandlerFunc  { return func(c *gin.Context) {} }
func RateLimit(n int) gin.HandlerFunc { return func(c *gin.Context) {} }

// SignMiddleware is the shape a real project's own guard takes: a struct with
// dependencies, a method returning the handler, and a name that reveals nothing
// to a pattern list. Its body is explicit about what it demands.
type SignMiddleware struct{ key string }

func (m *SignMiddleware) Handler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		platform := ctx.GetHeader("X-Platform")
		signature := ctx.GetHeader("X-Sign")
		timestamp := ctx.GetHeader("X-Time-Unix")
		requestID := ctx.GetHeader("X-Request-ID")

		if platform == "" || signature == "" || timestamp == "" {
			ctx.AbortWithStatusJSON(488, gin.H{"code": "InvalidSignature"})
			return
		}
		if requestID == "" {
			ctx.AbortWithStatusJSON(409, gin.H{"code": "DuplicateRequest"})
			return
		}
		if signature != m.key {
			ctx.AbortWithStatusJSON(498, gin.H{"code": "InvalidSign"})
			return
		}
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "Misconfigured"})
	}
}
