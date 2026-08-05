package nested

import (
	"encoding/json"
	"net/http"
)

type Item struct {
	ID int `json:"id"`
}

func requestID(next http.Handler) http.Handler { return next }

func apiKeyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}

// listItems returns every item.
func listItems(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]Item{})
}

// getItem returns one item.
func getItem(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Item{})
}

func Serve() {
	// v2 is mounted on v1, which is mounted on the root mux: two levels of
	// prefix, both of which must compose onto the leaf routes.
	v2 := http.NewServeMux()
	v2.HandleFunc("GET /items", listItems)
	v2.HandleFunc("GET /items/{id}", getItem)

	v1 := http.NewServeMux()
	v1.Handle("/v2/", http.StripPrefix("/v2", v2))

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", apiKeyGuard(http.StripPrefix("/api/v1", v1)))

	// requestID wraps the root handler, so it runs in front of every route.
	http.ListenAndServe(":8080", requestID(mux))
}
