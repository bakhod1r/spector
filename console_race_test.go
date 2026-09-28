package spector

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A rebuild triggered by one request must not rewrite the document another
// request is still serving. Run with -race.
func TestHandlerRebuildDoesNotRaceWithServing(t *testing.T) {
	noRecheckDelay(t)
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				for _, p := range []string{"/docs/openapi.json", "/docs/grpc.json", "/docs/graphql.json", "/docs/mock/widgets"} {
					w := httptest.NewRecorder()
					h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		edited := strings.Replace(ginSrc, `r.GET("/widgets"`, fmt.Sprintf(`r.GET("/w%d"`, i), 1)
		rewrite(t, filepath.Join(dir, "main.go"), edited)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil))
	}
	wg.Wait()
}

// A cross-site page can POST text/plain JSON without a preflight. The invoke
// endpoint makes the server dial a host the body names, so it must refuse a
// request no same-origin console would send.
func TestGrpcInvokeRefusesCrossSiteRequests(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir})
	body := `{"target":"169.254.169.254:80","method":"x.Y/Z"}`

	cases := []struct {
		name, ctype, origin string
	}{
		{"text/plain simple request", "text/plain", ""},
		{"foreign origin", "application/json", "https://evil.example"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke", strings.NewReader(body))
			req.Header.Set("Content-Type", c.ctype)
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want the request refused before any dial", w.Code)
			}
		})
	}
}

func TestGrpcInvokeBadBodyIs400(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir})
	req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
