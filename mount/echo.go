package mount

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/user/specter"
)

// Echo registers the console on an echo router under cfg.BasePath
// (default "/docs").
func Echo(e *echo.Echo, cfg specter.Config) {
	base, h := prepare(cfg)

	e.GET(base, func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently,
			redirectTarget(base, c.Request().URL.RawQuery))
	})

	wrapped := echo.WrapHandler(h)

	for _, ep := range endpoints {
		switch ep.method {
		case http.MethodPost:
			e.POST(base+ep.path, wrapped)
		default:
			e.GET(base+ep.path, wrapped)
		}
	}
}
