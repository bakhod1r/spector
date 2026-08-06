package localprefix

import "github.com/gofiber/fiber/v2"

func Routes(app *fiber.App) {
	base := "/v1"
	app.Get(base+"/categories", nil)
}
