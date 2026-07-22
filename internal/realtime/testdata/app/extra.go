package app

import "net/http"

type streamer struct{}

// The SSE setup lives in a helper reached through a method call.
func delegatingSSE(w http.ResponseWriter, r *http.Request) {
	var s streamer
	s.startStream(w)
}

func (s streamer) startStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
}

// A Set call with non-literal arguments is no SSE signal.
func headerFromVars(w http.ResponseWriter, r *http.Request) {
	k, v := "Content-Type", "application/json"
	w.Header().Set(k, v)
}
