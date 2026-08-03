package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Exchange is one request and its response, as the proxy saw them.
type Exchange struct {
	Time         time.Time   `json:"time"`
	Method       string      `json:"method"`
	Path         string      `json:"path"`
	Query        string      `json:"query,omitempty"`
	Status       int         `json:"status"`
	ReqHeader    http.Header `json:"requestHeaders,omitempty"`
	ResHeader    http.Header `json:"responseHeaders,omitempty"`
	RequestBody  []byte      `json:"-"`
	ResponseBody []byte      `json:"-"`

	// The bodies are written as raw JSON when they parse, and as a string when
	// they do not, so a recording stays readable either way.
	Request  json.RawMessage `json:"request,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
}

// Recorder writes exchanges to a file, one JSON object per line.
//
// SECURITY: a recording proxy captures whatever passes through it. On a real
// API that means bearer tokens, session cookies, API keys, passwords in login
// bodies, and personal data. A recording is therefore a credential store and a
// PII store, and it is very easy to commit one to a repository by accident.
//
// So the safe behaviour is the default and the unsafe one has to be asked for
// by name: credential headers are replaced with a placeholder unless Raw is
// set, fields with obviously sensitive names are masked in bodies, and the file
// is created 0600 rather than 0644.
//
// Masking bodies by field name is not a guarantee and is not presented as one.
// A field called "ssn" or "dob" is personal data that no name list will catch.
// The honest position, which the README states plainly, is that a recording of
// production traffic must be handled as production data.
type Recorder struct {
	mu  sync.Mutex
	w   io.WriteCloser
	enc *json.Encoder
	raw bool
	n   int
}

// NewRecorder writes exchanges to w. raw disables redaction.
func NewRecorder(w io.WriteCloser, raw bool) *Recorder {
	return &Recorder{w: w, enc: json.NewEncoder(w), raw: raw}
}

// Record writes one exchange.
func (r *Recorder) Record(ex Exchange) {
	if r == nil {
		return
	}
	ex.Time = time.Now().UTC()
	ex.Request = bodyValue(ex.RequestBody, r.raw)
	ex.Response = bodyValue(ex.ResponseBody, r.raw)
	if !r.raw {
		ex.ReqHeader = redactHeaders(ex.ReqHeader)
		ex.ResHeader = redactHeaders(ex.ResHeader)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enc.Encode(ex); err == nil {
		r.n++
	}
}

// Count is how many exchanges were written.
func (r *Recorder) Count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *Recorder) Close() error {
	if r == nil || r.w == nil {
		return nil
	}
	return r.w.Close()
}

const redacted = "<redacted by specter>"

// sensitiveHeaders are the headers that carry credentials outright.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-csrf-token":        true,
}

// sensitiveSubstrings catch the project-specific spellings: X-Api-Key,
// X-Auth-Token, X-Sign, X-Client-Secret. A header nobody standardised is still
// a credential when it is called one.
var sensitiveSubstrings = []string{"key", "token", "secret", "sign", "auth", "password", "credential"}

func redactHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for name, values := range h {
		if isSensitiveName(name) {
			out[name] = []string{redacted}
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	return out
}

func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	if sensitiveHeaders[lower] {
		return true
	}
	for _, s := range sensitiveSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// bodyValue prepares a body for the record, masking sensitive-looking fields.
//
// A body that is not JSON is stored as a string rather than dropped: a form
// post or a plain-text error is still what the endpoint answered, and the point
// of a recording is to be able to look at it later.
func bodyValue(body []byte, raw bool) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		s, mErr := json.Marshal(string(body))
		if mErr != nil {
			return nil
		}
		return s
	}
	if !raw {
		value = maskFields(value)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return out
}

// maskFields replaces the values of fields whose names say they are secrets.
//
// Only the value is replaced, never the key: knowing that a login request has a
// password field is exactly what makes a recording useful for debugging, and
// knowing what the password was is exactly what makes it dangerous.
func maskFields(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, item := range v {
			if isSensitiveName(name) {
				out[name] = redacted
				continue
			}
			out[name] = maskFields(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = maskFields(item)
		}
		return out
	}
	return value
}
