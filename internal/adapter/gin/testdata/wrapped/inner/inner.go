package inner

import "github.com/gin-gonic/gin"

// Register lives one directory below the scanned root: a scan that reads only
// the top directory documents none of it.
func Register(r *gin.Engine) {
	r.GET("/health", health)
}

func health(c *gin.Context) { c.JSON(200, gin.H{}) }
