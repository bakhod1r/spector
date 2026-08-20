package main

import (
	"fmt"
	"io"

	"github.com/bakhod1r/spector"
	"github.com/bakhod1r/spector/internal/evolve"
)

// evolveConfig is what the -evolve branch collected.
type evolveConfig struct {
	since          string
	baselineDir    string
	baselineJSON   string
	format         string
	failOnBreaking bool
}

// runEvolve compares the current document against a baseline and prints the
// report. The report goes to stdout so it can be piped or committed; the
// verdict and any error go to stderr; the exit code is the result, so a CI job
// can gate a merge on it.
func runEvolve(cfg spector.Config, ec evolveConfig, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "spector:", err)
		return 1
	}

	changes, err := spector.Evolve(cfg, spector.EvolveOptions{
		SinceRev:     ec.since,
		BaselineDir:  ec.baselineDir,
		BaselineJSON: ec.baselineJSON,
	})
	if err != nil {
		return fail(err)
	}

	switch ec.format {
	case "json":
		data, mErr := evolve.RenderJSON(changes)
		if mErr != nil {
			return fail(mErr)
		}
		if _, wErr := stdout.Write(data); wErr != nil {
			return fail(wErr)
		}
	case "markdown", "md":
		fmt.Fprint(stdout, evolve.RenderMarkdown(changes))
	case "text", "":
		fmt.Fprint(stdout, evolve.Render(changes))
	default:
		return fail(fmt.Errorf("unknown -evolve-format %q: expected text, json, or markdown", ec.format))
	}

	summary := evolve.Summarize(changes)
	if summary.Breaking > 0 {
		fmt.Fprintf(stderr, "spector: %d breaking change(s) found\n", summary.Breaking)
	}

	// -fail-on-breaking is the whole point of the flag: it turns a breaking
	// change into a non-zero exit, so a pipeline stops before the change ships.
	if ec.failOnBreaking && summary.Breaking > 0 {
		return 1
	}
	return 0
}
