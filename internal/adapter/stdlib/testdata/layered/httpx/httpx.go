// Package httpx is the helper package a net/http project grows: decode the
// body, then answer through one envelope.
package httpx

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func BindJSON[T any](w http.ResponseWriter, r *http.Request, dto *T) bool {
	if err := json.NewDecoder(r.Body).Decode(dto); err != nil {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(Envelope{Error: "invalid"})
		return false
	}
	return true
}

func OK(w http.ResponseWriter, data any) {
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(Envelope{Data: data})
}

func Created(w http.ResponseWriter, data any) {
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(Envelope{Data: data})
}
