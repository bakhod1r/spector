package mount

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/user/specter"
)

// Gin registers the console on a gin router under cfg.BasePath
// (default "/docs").
func Gin(r gin.IRouter, cfg specter.Config) {
	base, h := prepare(cfg)

	r.GET(base, func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, redirectTarget(base, c.Request.URL.RawQuery))
	})

	// A gin wildcard would collide with any route the application already has
	// under the same prefix, so each endpoint is registered by name.
	wrapped := gin.WrapH(h)

	for _, e := range endpoints {
		switch e.method {
		case http.MethodPost:
			r.POST(base+e.path, wrapped)
		default:
			r.GET(base+e.path, wrapped)
		}
	}
}
