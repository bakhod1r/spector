package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/user/specter"
	"github.com/user/specter/internal/proxy"
)

// proxyConfig is what the -proxy branch collected, so runProxy takes one value
// rather than nine arguments.
type proxyConfig struct {
	addr      string
	target    string
	report    string
	record    string
	recordRaw bool
	learn     string
	strict    bool
	title     string
	version   string
}

// runProxy starts the verifying proxy and blocks until interrupted.
//
// The exit code is decided after the run, not during it: a proxy is a
// long-running process, and -proxy-strict asks "did any drift happen while I
// was watching?", which can only be answered once watching stops. So it waits
// for a signal, prints a summary, writes what was asked for, and only then
// returns.
func runProxy(doc *specter.Document, cfg proxyConfig, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "specter:", err)
		return 1
	}

	opts := specter.ProxyOptions{
		Target: cfg.target,
		OnFinding: func(f proxy.Finding) {
			// Only the first sighting reaches here, so a busy endpoint does not
			// flood the terminal with the same line.
			fmt.Fprintln(stderr, "drift:", f.String())
		},
	}

	// Recording captures whatever passes through, which on a real API is
	// credentials. The warning is unconditional and the file is owner-only.
	var recorder *proxy.Recorder
	if cfg.record != "" {
		f, err := os.OpenFile(cfg.record, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return fail(err)
		}
		defer f.Close()
		recorder = proxy.NewRecorder(f, cfg.recordRaw)
		opts.Recorder = recorder

		fmt.Fprintf(stderr, "specter: recording traffic to %s — this file will contain request and response bodies.\n", cfg.record)
		if cfg.recordRaw {
			fmt.Fprintln(stderr, "specter: WARNING: -proxy-record-raw is on, so credentials and sensitive fields are NOT redacted. Do not commit this file.")
		} else {
			fmt.Fprintln(stderr, "specter: credential headers and sensitive-looking fields are redacted, but bodies may still hold personal data. Do not commit this file.")
		}
	}

	var learner *proxy.Learner
	if cfg.learn != "" {
		learner = proxy.NewLearner()
		opts.Learner = learner
	}

	p, err := specter.NewProxy(doc, opts)
	if err != nil {
		return fail(err)
	}

	server := &http.Server{Addr: cfg.addr, Handler: p.Handler()}

	// A signal ends the run cleanly, which is the normal way to stop a proxy —
	// so it is not an error, and the summary and reports are written on the way
	// out rather than being lost to a bare exit.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- server.ListenAndServe() }()

	fmt.Fprintf(stderr, "specter: proxying %s -> %s (%d documented paths)\n", cfg.addr, cfg.target, len(doc.Paths))
	fmt.Fprintln(stderr, "specter: forwarding everything; reporting only where traffic disagrees with the document. Ctrl-C to stop.")

	select {
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			return fail(err)
		}
	case <-ctx.Done():
		fmt.Fprintln(stderr, "\nspecter: stopping…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}

	// Summary: what disagreed, most frequent first, against how much was seen —
	// zero findings over zero requests would mean nothing.
	findings := p.Findings()
	fmt.Fprintf(stderr, "\nspecter: %d request(s) observed, %d distinct finding(s)\n", p.Requests(), len(findings))
	for _, f := range findings {
		fmt.Fprintf(stderr, "  [%d×] %s\n", f.Count, f.String())
	}

	if cfg.report != "" {
		data, mErr := json.MarshalIndent(p.Report(cfg.target), "", "  ")
		if mErr != nil {
			return fail(mErr)
		}
		if wErr := os.WriteFile(cfg.report, append(data, '\n'), 0o644); wErr != nil {
			return fail(wErr)
		}
		fmt.Fprintf(stderr, "specter: wrote drift report to %s\n", cfg.report)
	}

	if recorder != nil {
		fmt.Fprintf(stderr, "specter: recorded %d exchange(s) to %s\n", recorder.Count(), cfg.record)
	}

	if learner != nil {
		if learner.Empty() {
			fmt.Fprintln(stderr, "specter: nothing to learn — every request matched a documented endpoint")
		} else {
			data, mErr := json.MarshalIndent(learner.Document(cfg.title, cfg.version), "", "  ")
			if mErr != nil {
				return fail(mErr)
			}
			if wErr := os.WriteFile(cfg.learn, append(data, '\n'), 0o644); wErr != nil {
				return fail(wErr)
			}
			fmt.Fprintf(stderr, "specter: wrote an OpenAPI fragment for undocumented traffic to %s (review before merging)\n", cfg.learn)
		}
	}

	// -proxy-strict makes drift a build failure. The exit code is the whole
	// point of the flag, so it comes last, after everything is written.
	if cfg.strict && len(findings) > 0 {
		return 1
	}
	return 0
}
