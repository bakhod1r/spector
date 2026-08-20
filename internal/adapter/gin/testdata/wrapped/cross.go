package wrapped

import (
	"github.com/bakhod1r/spector/internal/adapter/gin/testdata/wrapped/httpx"
	"github.com/gin-gonic/gin"
)

type RoleReq struct {
	Name string `json:"name"`
}

type RoleResp struct {
	ID string `json:"id"`
}

// newRoleResp is how a response is built in practice: a constructor, not a
// composite literal at the call site. The result type is in the signature, so
// running it is not required to know it.
func newRoleResp() RoleResp { return RoleResp{} }

func registerRoles(g *gin.RouterGroup) {
	g.POST("/roles", createRole)
}

func createRole(c *gin.Context) {
	var req RoleReq
	if !httpx.BindJSON(c, &req) {
		return
	}
	httpx.Created(c, newRoleResp())
}
