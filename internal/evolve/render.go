package evolve

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Render writes the changes as one line each, breaking first, the way -lint
// reports routing problems. It is what a person reads in a terminal.
func Render(changes []Change) string {
	if len(changes) == 0 {
		return "No API changes.\n"
	}
	var b strings.Builder
	for _, c := range changes {
		b.WriteString(c.String())
		b.WriteByte('\n')
	}
	s := Summarize(changes)
	fmt.Fprintf(&b, "\n%d breaking, %d compatible, %d addition(s).\n", s.Breaking, s.Compatible, s.Addition)
	return b.String()
}

// report is the JSON shape: a verdict a machine can gate on, plus the detail.
type report struct {
	Summary Summary  `json:"summary"`
	Changes []Change `json:"changes"`
}

// RenderJSON writes a machine-readable diff, stable enough for CI to compare two
// of them.
func RenderJSON(changes []Change) ([]byte, error) {
	if changes == nil {
		changes = []Change{}
	}
	data, err := json.MarshalIndent(report{Summary: Summarize(changes), Changes: changes}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// RenderMarkdown writes a changelog section, grouped by severity, to drop into a
// release note.
func RenderMarkdown(changes []Change) string {
	var b strings.Builder
	b.WriteString("## API changes\n\n")
	if len(changes) == 0 {
		b.WriteString("No API changes.\n")
		return b.String()
	}

	groups := []struct {
		severity, heading, note string
	}{
		{Breaking, "⚠️ Breaking", "These change behaviour an existing client depends on."},
		{Compatible, "Compatible", "Safe to adopt without changing existing clients."},
		{Addition, "Additions", "New surface; nothing existing is affected."},
	}

	for _, g := range groups {
		var lines []Change
		for _, c := range changes {
			if c.Severity == g.severity {
				lines = append(lines, c)
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", g.heading, g.note)
		for _, c := range lines {
			loc := c.Path
			if c.Method != "" {
				loc = "`" + strings.ToUpper(c.Method) + " " + c.Path + "`"
			}
			if loc == "" {
				fmt.Fprintf(&b, "- %s\n", c.Detail)
			} else {
				fmt.Fprintf(&b, "- %s — %s\n", loc, c.Detail)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
