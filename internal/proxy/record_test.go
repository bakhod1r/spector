package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type buffer struct{ bytes.Buffer }

func (b *buffer) Close() error { return nil }

func recorded(t *testing.T, raw bool, ex Exchange) map[string]any {
	t.Helper()
	var buf buffer
	r := NewRecorder(&buf, raw)
	r.Record(ex)

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("the record is not JSON: %v\n%s", err, buf.String())
	}
	return out
}

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

// A recording proxy captures whatever passes through it, which on a real API
// means credentials. Redaction is therefore the default, and these tests are
// the reason it can be trusted to be.
func TestCredentialHeadersAreRedactedByDefault(t *testing.T) {
	got := recorded(t, false, Exchange{
		Method: "GET", Path: "/users",
		ReqHeader: headers(
			"Authorization", "Bearer sk-live-abcdef",
			"Cookie", "session=secret-value",
			"X-Api-Key", "key-12345",
			"X-Request-Id", "req-1",
		),
	})

	blob, _ := json.Marshal(got)
	for _, secret := range []string{"sk-live-abcdef", "secret-value", "key-12345"} {
		if strings.Contains(string(blob), secret) {
			t.Errorf("a credential was written to the record: %s", secret)
		}
	}
	// An ordinary header is kept: a recording with nothing in it is no use.
	if !strings.Contains(string(blob), "req-1") {
		t.Error("an ordinary header was redacted")
	}
}

// Set-Cookie hands out a session; it is as sensitive on the way back as Cookie
// is on the way in.
func TestResponseCredentialHeadersAreRedacted(t *testing.T) {
	got := recorded(t, false, Exchange{
		Method: "POST", Path: "/login",
		ResHeader: headers("Set-Cookie", "session=abc123; HttpOnly"),
	})
	blob, _ := json.Marshal(got)
	if strings.Contains(string(blob), "abc123") {
		t.Errorf("a session cookie was written to the record: %s", blob)
	}
}

// A password is only recognisable by the name of the field it is in.
func TestSensitiveBodyFieldsAreMasked(t *testing.T) {
	got := recorded(t, false, Exchange{
		Method: "POST", Path: "/login",
		RequestBody: []byte(`{"email":"ada@example.com","password":"hunter2","nested":{"apiKey":"k-1"}}`),
	})

	blob, _ := json.Marshal(got["request"])
	for _, secret := range []string{"hunter2", "k-1"} {
		if strings.Contains(string(blob), secret) {
			t.Errorf("a secret survived masking: %s in %s", secret, blob)
		}
	}
	// The field name stays: knowing a login sends a password is what makes the
	// record useful, knowing what it was is what makes it dangerous.
	if !strings.Contains(string(blob), "password") {
		t.Error("the field name was removed along with the value")
	}
	if !strings.Contains(string(blob), "ada@example.com") {
		t.Error("an ordinary field was masked")
	}
}

// Turning redaction off has to be possible — a debugging session on a local
// API is a real need — but it is asked for by name, never assumed.
func TestRawRecordingKeepsEverything(t *testing.T) {
	got := recorded(t, true, Exchange{
		Method:      "POST",
		Path:        "/login",
		ReqHeader:   headers("Authorization", "Bearer sk-live-abcdef"),
		RequestBody: []byte(`{"password":"hunter2"}`),
	})
	blob, _ := json.Marshal(got)
	if !strings.Contains(string(blob), "sk-live-abcdef") || !strings.Contains(string(blob), "hunter2") {
		t.Errorf("raw mode dropped something: %s", blob)
	}
}

// A body that is not JSON is still what the endpoint answered.
func TestNonJSONBodiesAreKeptAsText(t *testing.T) {
	got := recorded(t, false, Exchange{
		Method: "GET", Path: "/health", ResponseBody: []byte("OK"),
	})
	if str, ok := got["response"].(string); !ok || str != "OK" {
		t.Errorf("response = %#v, want the text kept as a string", got["response"])
	}
}

func TestEmptyBodiesAreOmitted(t *testing.T) {
	got := recorded(t, false, Exchange{Method: "DELETE", Path: "/users/1", Status: 204})
	if _, present := got["request"]; present {
		t.Error("an empty request body was written")
	}
	if _, present := got["response"]; present {
		t.Error("an empty response body was written")
	}
}

// One JSON object per line, so a recording can be grepped, split, and streamed
// rather than needing to be parsed whole.
func TestRecordsAreOnePerLine(t *testing.T) {
	var buf buffer
	r := NewRecorder(&buf, false)
	for i := 0; i < 3; i++ {
		r.Record(Exchange{Method: "GET", Path: "/users", Status: 200})
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Errorf("line %d is not JSON: %v", i, err)
		}
	}
	if r.Count() != 3 {
		t.Errorf("count = %d, want 3", r.Count())
	}
}

// The recorder is fed from the proxy, so the wiring has to work end to end.
func TestProxyFeedsTheRecorder(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"id":1,"name":"ada"}]`)
	})
	var buf buffer
	rec := NewRecorder(&buf, false)
	_, s := front(t, upstream.URL, Options{Recorder: rec})

	call(t, s, "GET", "/users")

	if rec.Count() != 1 {
		t.Fatalf("recorded %d exchanges, want 1", rec.Count())
	}
	if !strings.Contains(buf.String(), `"ada"`) {
		t.Errorf("the response body was not recorded:\n%s", buf.String())
	}
}

// A nil recorder is the normal case — recording is opt-in — and must not be a
// special case at every call site.
func TestNilRecorderIsSafe(t *testing.T) {
	var r *Recorder
	r.Record(Exchange{Method: "GET"})
	if r.Count() != 0 || r.Close() != nil {
		t.Error("a nil recorder should be inert")
	}
}
