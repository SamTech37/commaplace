package markdown

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/ofm/*.html from the current renderer")

// TestOFMGolden renders every testdata/ofm/*.md as alice's note, with no
// resolver or embeds, and compares against the .html beside it. After an
// intended change: go test ./internal/markdown -run OFMGolden -update, then
// review the diff.
func TestOFMGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/ofm/*.md")
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".md")
		t.Run(name, func(t *testing.T) {
			md, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			got := render(t, string(md))
			golden := strings.TrimSuffix(in, ".md") + ".html"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if got != string(want) {
				t.Errorf("%s changed.\n--- got\n%s\n--- want\n%s", name, got, want)
			}
		})
	}
}
