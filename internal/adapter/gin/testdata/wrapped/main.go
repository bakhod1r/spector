package wrapped

import "github.com/gin-gonic/gin"

type LoginReq struct {
	Email string `json:"email"`
}

type TokenResp struct {
	Token string `json:"token"`
}

type CreatedResp struct {
	ID string `json:"id"`
}

func Register(r *gin.Engine) {
	private := r.Group("/api/v1")
	registerMFA(private.Group("/auth/mfa"))
	admin := private.Group("/admin")
	registerBilling(admin.Group("/billing"))
	registerRoles(private.Group("/rbac"))
}

func registerMFA(g *gin.RouterGroup) {
	g.POST("/enroll", enroll)
	g.GET("/dynamic", dynamic)
}

func registerBilling(b *gin.RouterGroup) {
	b.POST("/invoices", createInvoice)
	b.GET("/list", listInvoices)
	registerRefunds(b.Group("/refunds"))
}

// registerRefunds names its group r, the same name Register gives the root
// engine. Resolving parameter names globally would put /api/v1/admin/billing
// in front of every route registered on the root router.
func registerRefunds(r *gin.RouterGroup) {
	r.GET("/pending", listInvoices)
}

func bindJSON(c *gin.Context, out any) bool { return c.ShouldBindJSON(out) == nil }

func ok(c *gin.Context, body any) { c.JSON(200, body) }

func created(c *gin.Context, body any) { c.JSON(201, body) }

func respond(c *gin.Context, code int, body any) { c.JSON(code, body) }

func enroll(c *gin.Context) {
	var req LoginReq
	if !bindJSON(c, &req) {
		return
	}
	ok(c, TokenResp{})
}

func createInvoice(c *gin.Context) {
	var req LoginReq
	if !bindJSON(c, &req) {
		return
	}
	created(c, CreatedResp{})
}

func listInvoices(c *gin.Context) {
	_ = c.DefaultQuery("limit", "20")
	ok(c, []TokenResp{})
}

func dynamic(c *gin.Context) {
	respond(c, statusFor(), TokenResp{})
}

func statusFor() int { return 418 }
