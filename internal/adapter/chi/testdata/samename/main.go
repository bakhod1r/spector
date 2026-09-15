// Two handler types carrying the same method names, which is the ordinary way
// a Go service is laid out and the shape that made the scan resolve a handler
// by bare name.
package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Product struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ProductPage struct {
	Items []Product `json:"items"`
	Total int       `json:"total"`
}

type Order struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type ProductHandler struct{}

// Get returns one product.
func (h *ProductHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Product{})
}

// List returns a page of products.
func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	_ = page
	out := ProductPage{}
	json.NewEncoder(w).Encode(out)
}

type OrderHandler struct{}

// Get returns one order.
func (h *OrderHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Order{})
}

func main() {
	products := &ProductHandler{}
	orders := NewOrderHandler()

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Route("/products", func(r chi.Router) {
			r.Get("/", products.List)
			r.Get("/{productID}", products.Get)
		})
		r.Get("/orders/{orderID}", orders.Get)
	})
	http.ListenAndServe(":8080", r)
}

func NewOrderHandler() *OrderHandler { return &OrderHandler{} }
