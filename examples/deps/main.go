// Package main is a small service whose handlers actually reach out: a
// database, a cache, another service over HTTP, and a queue. examples/shop is
// deliberately in-memory, so it has no dependencies to draw; this one exists to
// exercise the call graph.
package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/user/specter"
	"github.com/user/specter/mount"
)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type Order struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Total  int    `json:"total"`
}

// The cache and queue are interfaces rather than a real redis/kafka client so
// this example adds no dependencies to the module. That also makes it the
// honest demonstration: with no import to go by, Specter has only the field
// names to work from, and reports those findings as "likely" rather than
// "certain".
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
}

type Queue interface {
	WriteMessages(ctx context.Context) error
}

type Server struct {
	db     *sql.DB
	cache  Cache
	writer Queue
}

// getUser reads straight from the database.
func (s *Server) getUser(c *gin.Context) {
	row := s.db.QueryRowContext(c, "select id, email from users where id = $1", c.Param("id"))
	var u User
	_ = row.Scan(&u.ID, &u.Email)
	c.JSON(http.StatusOK, u)
}

// listOrders goes through a service layer, which is the shape most real
// handlers have: the dependency is two calls below the handler.
func (s *Server) listOrders(c *gin.Context) {
	c.JSON(http.StatusOK, s.ordersFor(c, c.Query("userId")))
}

func (s *Server) ordersFor(ctx context.Context, userID string) []Order {
	return s.queryOrders(ctx, userID)
}

func (s *Server) queryOrders(ctx context.Context, userID string) []Order {
	rows, err := s.db.QueryContext(ctx, "select id, user_id, total from orders where user_id = $1", userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return nil
}

// checkout touches everything at once: it reads the cache, writes the database,
// charges an external payment service, and publishes an event.
func (s *Server) checkout(c *gin.Context) {
	s.cache.Get(c, "cart:"+c.Param("id"))
	s.db.ExecContext(c, "insert into orders values ($1)", c.Param("id"))

	resp, err := http.Post("https://payments.example.com/charge", "application/json", nil)
	if err == nil {
		defer resp.Body.Close()
	}

	s.writer.WriteMessages(c)
	c.JSON(http.StatusAccepted, Order{ID: c.Param("id")})
}

// health reaches nothing, and must be reported as reaching nothing.
func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func router(s *Server) *gin.Engine {
	r := gin.Default()
	api := r.Group("/api")
	api.GET("/users/:id", s.getUser)
	api.GET("/orders", s.listOrders)
	api.POST("/checkout/:id", s.checkout)
	api.GET("/health", s.health)
	return r
}

func main() {
	s := &Server{}
	r := router(s)

	mount.Gin(r, specter.Config{
		Dir:     ".",
		Title:   "Deps API",
		Version: "1.0.0",
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8090"
	}
	r.Run(addr)
}
