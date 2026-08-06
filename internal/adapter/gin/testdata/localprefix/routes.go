package localprefix

import "github.com/gin-gonic/gin"

func Routes(r *gin.Engine) {
	base := "/v1"
	r.GET(base+"/categories", nil)
}
