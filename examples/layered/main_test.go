package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router().ServeHTTP(w, req)
	return w
}

// envelope mirrors httpx.Envelope for assertions, kept local so the test reads
// the wire format rather than the Go type.
type envelope struct {
	Data  json.RawMessage           `json:"data"`
	Meta  *struct{ Total int }      `json:"meta"`
	Error *struct{ Message string } `json:"error"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body is not an envelope: %v (%s)", err, w.Body.String())
	}
	return e
}

// The prefix reaches these routes through a parameter, a bare sub-group and
// another package's method. If any hop drops it the paths 404.
func TestPrefixSurvivesEveryHop(t *testing.T) {
	for _, path := range []string{"/api/v1/users", "/api/v1/orders"} {
		if w := do(t, http.MethodGet, path, ""); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}

// Both contexts declare Create. They must remain two different endpoints.
func TestCollidingNamesAreDifferentEndpoints(t *testing.T) {
	w := do(t, http.MethodPost, "/api/v1/users", `{"email":"grace@example.com","name":"Grace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", w.Code, w.Body.String())
	}
	if got := decode(t, w).Data; !strings.Contains(string(got), "grace@example.com") {
		t.Errorf("user payload = %s", got)
	}

	w = do(t, http.MethodPost, "/api/v1/orders", `{"user_id":"1","total":999}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create order = %d: %s", w.Code, w.Body.String())
	}
	if got := decode(t, w).Data; !strings.Contains(string(got), `"status":"pending"`) {
		t.Errorf("order payload = %s", got)
	}
}

// The handler factory is registered twice with different arguments; each route
// has to answer with its own channel.
func TestHandlerFactoryPerRoute(t *testing.T) {
	for _, ch := range []string{"email", "phone"} {
		w := do(t, http.MethodPost, "/api/v1/users/1/verify/"+ch, `{"code":"000000"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("verify %s = %d: %s", ch, w.Code, w.Body.String())
		}
		if got := decode(t, w).Data; !strings.Contains(string(got), `"channel":"`+ch+`"`) {
			t.Errorf("verify %s payload = %s", ch, got)
		}
	}
}

// The 204 is written three calls out of the handler body.
func TestDeleteAnswers204(t *testing.T) {
	if w := do(t, http.MethodDelete, "/api/v1/users/1", ""); w.Code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", w.Code)
	}
}

// Binding failures are answered by the helper package, not the handler.
func TestBindFailureIsAnEnvelope(t *testing.T) {
	w := do(t, http.MethodPost, "/api/v1/users", `{"email":"nope"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	if e := decode(t, w); e.Error == nil {
		t.Errorf("no error in envelope: %s", w.Body.String())
	}
}

// The list endpoint carries paging metadata alongside the payload.
func TestListCarriesMeta(t *testing.T) {
	w := do(t, http.MethodGet, "/api/v1/users", "")
	e := decode(t, w)
	if e.Meta == nil || e.Meta.Total == 0 {
		t.Errorf("no meta: %s", w.Body.String())
	}
}
