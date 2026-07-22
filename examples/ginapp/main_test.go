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

func TestListUsers(t *testing.T) {
	w := do(t, http.MethodGet, "/api/v1/users", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out []User
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a user list: %v", err)
	}
	if len(out) != len(users) {
		t.Errorf("users = %d, want %d", len(out), len(users))
	}
}

// The q filter matches a case-insensitive substring of the name.
func TestListUsersFilter(t *testing.T) {
	cases := []struct {
		q    string
		want int
	}{
		{"ada", 1},
		{"ADA", 1},
		{"a", 2},
		{"nobody", 0},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			w := do(t, http.MethodGet, "/api/v1/users?q="+tc.q, "")
			var out []User
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out) != tc.want {
				t.Errorf("users = %d, want %d", len(out), tc.want)
			}
		})
	}
}

// limit is read but not applied; the endpoint must still answer normally.
func TestListUsersAcceptsLimit(t *testing.T) {
	if w := do(t, http.MethodGet, "/api/v1/users?limit=1", ""); w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetUser(t *testing.T) {
	w := do(t, http.MethodGet, "/api/v1/users/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var u User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if u.Name != "Ada" {
		t.Errorf("name = %q, want Ada", u.Name)
	}
}

func TestGetUserNotFound(t *testing.T) {
	w := do(t, http.MethodGet, "/api/v1/users/999", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] == "" {
		t.Error("404 carries no error message")
	}
}

// A non-numeric id parses as 0, which is not a known user.
func TestGetUserNonNumericID(t *testing.T) {
	if w := do(t, http.MethodGet, "/api/v1/users/abc", ""); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestCreateUser(t *testing.T) {
	w := do(t, http.MethodPost, "/api/v1/users", `{"name":"Grace","email":"grace@example.com"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var u User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if u.Name != "Grace" || u.Email != "grace@example.com" {
		t.Errorf("= %+v, want the input echoed", u)
	}
	if u.ID == 0 {
		t.Error("no id assigned")
	}
}

func TestCreateUserRejectsMalformedJSON(t *testing.T) {
	w := do(t, http.MethodPost, "/api/v1/users", "{not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] == "" {
		t.Error("400 carries no error message")
	}
}

// The console is mounted alongside the API.
func TestDocsMounted(t *testing.T) {
	if w := do(t, http.MethodGet, "/docs/", ""); w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w := do(t, http.MethodGet, "/docs/openapi.json", ""); !json.Valid(w.Body.Bytes()) {
		t.Error("openapi.json is not valid JSON")
	}
}
