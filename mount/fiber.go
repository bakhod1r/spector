package mount

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/user/specter"
)

// Fiber registers the console on a fiber app under cfg.BasePath
// (default "/docs").
//
// fiber runs on fasthttp rather than net/http, so every request crosses an
// adaptor that rebuilds it as an *http.Request. That copies the body, which is
// fine for a documentation console but is worth knowing if you mount it on a
// hot path.
func Fiber(app fiber.Router, cfg specter.Config) {
	base, h := prepare(cfg)

	// fiber is non-strict by default, so a route for "/docs" also matches
	// "/docs/" — redirecting unconditionally here would trap the console in a
	// loop. Only the slash-less form redirects; the rest falls through to the
	// wildcard registered below.
	app.Get(base, func(c *fiber.Ctx) error {
		if strings.HasSuffix(c.Path(), "/") {
			return c.Next()
		}
		return c.Redirect(redirectTarget(base, string(c.Request().URI().QueryString())),
			http.StatusMovedPermanently)
	})

	wrapped := adaptor.HTTPHandler(h)
	app.Get(base+"/*", wrapped)
	app.Post(base+"/*", wrapped)
}
