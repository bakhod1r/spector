package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

// Query parameters and form bodies carry credentials as often as JSON does.
func TestRecorderRedactsQueryAndFormBody(t *testing.T) {
	var buf bytes.Buffer
	r := NewRecorder(nopCloser{&buf}, false)
	h := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	r.Record(Exchange{
		Method: "POST", Path: "/login", Query: "api_key=QSECRET&page=2",
		ReqHeader: h, RequestBody: []byte("user=ann&password=FSECRET"),
	})
	out := buf.String()
	if strings.Contains(out, "QSECRET") || strings.Contains(out, "FSECRET") {
		t.Fatalf("recording leaked a credential: %s", out)
	}
	if !strings.Contains(out, "page=2") || !strings.Contains(out, "ann") {
		t.Fatalf("recording lost non-secret values: %s", out)
	}
}

// A long-running proxy under scanner traffic sees endless distinct paths. The
// findings kept must stay bounded.
func TestFindingsAreBounded(t *testing.T) {
	p := &Proxy{findings: map[string]*Finding{}}
	for i := 0; i < maxFindings+500; i++ {
		p.report(Options{}, Finding{Kind: KindUndocumentedEndpoint, Method: "GET",
			Path: fmt.Sprintf("/scan/%d", i), Detail: "x"}, "/")
	}
	if n := len(p.Findings()); n > maxFindings {
		t.Fatalf("%d findings kept, want at most %d", n, maxFindings)
	}
	if p.Dropped() != 500 {
		t.Fatalf("Dropped = %d, want 500", p.Dropped())
	}
}

func TestLearnerIsBounded(t *testing.T) {
	l := NewLearner()
	for i := 0; i < maxLearned+100; i++ {
		l.Observe(Exchange{Method: fmt.Sprintf("M%d", i), Path: "/x", Status: 200}, "")
	}
	l.mu.Lock()
	n := len(l.seen)
	l.mu.Unlock()
	if n > maxLearned {
		t.Fatalf("learner kept %d endpoints, want at most %d", n, maxLearned)
	}
}
