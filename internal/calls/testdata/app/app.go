package app

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

type Server struct {
	db     *sql.DB
	rdb    *redis.Client
	writer *kafka.Writer
}

// Direct: the handler queries the database itself.
func (s *Server) getUser(ctx context.Context, id string) {
	s.db.QueryRowContext(ctx, "select 1")
}

// Delegating: the dependency is two calls down, which is the shape almost
// every real service has.
func (s *Server) listOrders(ctx context.Context) {
	s.loadOrders(ctx)
}

func (s *Server) loadOrders(ctx context.Context) {
	s.queryOrders(ctx)
}

func (s *Server) queryOrders(ctx context.Context) {
	s.db.QueryContext(ctx, "select 2")
}

// Several kinds at once.
func (s *Server) checkout(ctx context.Context) {
	s.db.ExecContext(ctx, "insert")
	s.rdb.Set(ctx, "k", "v", 0)
	s.writer.WriteMessages(ctx)
	http.Get("https://payments.example.com/charge")
}

// Recursion must not hang the walk.
func (s *Server) recurse(ctx context.Context) {
	s.recurse(ctx)
	s.db.Query("select 3")
}

// A handler that reaches nothing must report nothing rather than every method
// call it happens to make.
func (s *Server) ping() string {
	m := map[string]string{"a": "b"}
	_ = m["a"]
	return sanitize("pong")
}

func sanitize(s string) string { return s }

// Traps: names and methods that look like dependencies but are not.
func (s *Server) traps(ctx context.Context) {
	var debug struct{ String func() string }
	_ = debug
	cache := map[string]string{}
	_ = cache["x"] // a map index is not a cache read
}
