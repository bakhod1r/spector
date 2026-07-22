package app

import (
	"context"
	"fmt"
	"net/http"

	rds "github.com/redis/go-redis/v9"
)

type Extra struct {
	client *http.Client
	cache  *rds.Client
}

// A named import still resolves: rds is redis.
func (e *Extra) namedImport(ctx context.Context) {
	e.cache.Get(ctx, "k")
}

// Receiver-name conventions: a field called client doing a request.
func (e *Extra) callsOut(req *http.Request) {
	e.client.Do(req)
}

// Shapes that must classify as nothing.
func oddShapes() {
	fmt.Sprintf("an import that is no dependency")
	http.NewServeMux()          // net/http, but server-side plumbing
	newClient().Do(nil)         // receiver is a call, no name to judge
	missingHelper()             // declared nowhere in the package
	deep1()
}

func newClient() *http.Client { return http.DefaultClient }

// Two calls of the same kind force the sort's target tie-break.
func (s *Server) twoQueries(ctx context.Context) {
	s.db.QueryContext(ctx, "a")
	s.db.ExecContext(ctx, "b")
}

// A chain longer than maxDepth: the walk must stop, not descend forever.
func deep1() { deep2() }
func deep2() { deep3() }
func deep3() { deep4() }
func deep4() { deep5() }
func deep5() {
	var db interface{ Query(string) }
	db.Query("too deep to be seen")
}
