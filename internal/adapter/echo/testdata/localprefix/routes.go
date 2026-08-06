package localprefix

import "github.com/labstack/echo/v4"

func Routes(e *echo.Echo) {
	base := "/v1"
	e.GET(base+"/categories", nil)
}
