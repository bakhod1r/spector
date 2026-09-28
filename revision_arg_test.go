package spector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A revision is handed to git as an argument. One that starts with "-" is an
// option, and git archive's --output writes wherever it names.
func TestRevisionThatLooksLikeAnOptionIsRefused(t *testing.T) {
	out := filepath.Join(t.TempDir(), "pwned")
	_, err := revisionDoc(Config{Dir: "."}, "--output="+out)
	if err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("err = %v, want the revision refused", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("git wrote the file an option-shaped revision named")
	}
}
