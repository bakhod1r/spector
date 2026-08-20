package wrapped

import "github.com/gin-gonic/gin"

// testRouter is a test fixture, not the API. A scan that reads _test.go files
// documents GET / and collides with the real routes.
func testRouter() *gin.Engine {
	r := gin.New()
	r.GET("/", func(c *gin.Context) {})
	return r
}
