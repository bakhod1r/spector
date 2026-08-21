package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The proxy runs until interrupted, so a test drives it on a goroutine, sends
// the traffic it wants observed, and then signals the process to stop the same
// way a person would — which is also the path that writes the reports.
func runProxyCLI(t *testing.T, upstream string, extraArgs []string, traffic func(base string)) (int, string) {
	t.Helper()

	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	addr := "127.0.0.1:" + freePort(t)

	args := append([]string{"-dir", dir, "-proxy", addr, "-proxy-target", upstream}, extraArgs...)

	var stderr strings.Builder
	done := make(chan int, 1)
	go func() { done <- run(args, io.Discard, &safeWriter{w: &stderr}) }()

	base := "http://" + addr
	waitForListener(t, base)
	traffic(base)
	// The proxy inspects the last response after the client already has it,
	// so the interrupt has to wait for that to finish or the report is written
	// without it.
	settle(t, base)

	interruptSelf(t)

	select {
	case code := <-done:
		return code, stderr.String()
	case <-time.After(10 * time.Second):
		t.Fatal("the proxy did not stop after the interrupt")
		return 0, ""
	}
}

func TestProxyReportsUndocumentedTraffic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	report := filepath.Join(t.TempDir(), "drift.json")
	code, stderr := runProxyCLI(t, upstream.URL, []string{"-proxy-report", report}, func(base string) {
		// The gin fixture documents GET /widgets; this is neither.
		sendGet(t, base+"/internal/secret")
	})
	if code != 0 {
		t.Fatalf("exit = %d (no -proxy-strict), stderr:\n%s", code, stderr)
	}

	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Findings []struct {
			Kind, Path string
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range rep.Findings {
		if f.Kind == "undocumented-endpoint" && f.Path == "/internal/secret" {
			found = true
		}
	}
	if !found {
		t.Errorf("report did not name the undocumented endpoint: %s", data)
	}
}

// -proxy-strict turns drift into a build failure, which is the whole reason the
// flag exists.
func TestProxyStrictExitsNonZeroOnDrift(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	code, stderr := runProxyCLI(t, upstream.URL, []string{"-proxy-strict"}, func(base string) {
		sendGet(t, base+"/internal/secret")
	})
	if code != 1 {
		t.Errorf("exit = %d, want 1 under -proxy-strict with drift\nstderr:\n%s", code, stderr)
	}
}

// A recording is a credential store, so the warning must be unmissable and the
// file owner-only.
func TestProxyRecordWarnsAndLocksTheFile(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	rec := filepath.Join(t.TempDir(), "traffic.jsonl")
	_, stderr := runProxyCLI(t, upstream.URL, []string{"-proxy-record", rec}, func(base string) {
		sendGet(t, base+"/widgets")
	})

	if !strings.Contains(stderr, "Do not commit") {
		t.Errorf("no do-not-commit warning was printed:\n%s", stderr)
	}
	info, err := os.Stat(rec)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("record file mode = %v, want 0600", perm)
	}
}

// The learner writes a fragment only for what the document lacks, and marks it
// as needing review.
func TestProxyLearnWritesAReviewableFragment(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"queued":true}`)
	}))
	defer upstream.Close()

	learn := filepath.Join(t.TempDir(), "observed.json")
	_, stderr := runProxyCLI(t, upstream.URL, []string{"-proxy-learn", learn}, func(base string) {
		sendPost(t, base+"/internal/reindex", "application/json", "{}")
	})

	data, err := os.ReadFile(learn)
	if err != nil {
		t.Fatalf("no fragment written: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(string(data), "/internal/reindex") {
		t.Errorf("the observed endpoint is missing from the fragment:\n%s", data)
	}
	if !strings.Contains(string(data), "review") {
		t.Errorf("the fragment does not ask to be reviewed:\n%s", data)
	}
}

// A missing target is caught before anything runs.
func TestProxyWithoutATargetFails(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-proxy", "127.0.0.1:"+freePort(t))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "target") {
		t.Errorf("stderr = %q, want the missing target explained", stderr)
	}
}
