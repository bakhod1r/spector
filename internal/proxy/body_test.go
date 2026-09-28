package proxy

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"testing"
)

// The proxy promises to forward traffic untouched. A body larger than what it
// keeps for inspection must still reach the API whole.
func TestProxyForwardsLargeRequestBodyWhole(t *testing.T) {
	body := bytes.Repeat([]byte("0123456789abcdef"), (3<<20)/16) // 3 MiB
	want := sha256.Sum256(body)

	var got [32]byte
	var gotLen int
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotLen = len(b)
		got = sha256.Sum256(b)
		w.WriteHeader(http.StatusNoContent)
	})
	_, s := front(t, upstream.URL, Options{})

	req, _ := http.NewRequest(http.MethodPost, s.URL+"/users", bytes.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if gotLen != len(body) || got != want {
		t.Fatalf("upstream got %d bytes, want %d intact", gotLen, len(body))
	}
}
