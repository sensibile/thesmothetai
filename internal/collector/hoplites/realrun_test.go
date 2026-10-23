package hoplites

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestRealRuns resolves every record of the Hoplites runs under
// $HOPLITES_ARTIFACTS (glob). Skipped when unset.
func TestRealRuns(t *testing.T) {
	pattern := os.Getenv("HOPLITES_ARTIFACTS")
	if pattern == "" {
		t.Skip("HOPLITES_ARTIFACTS not set")
	}
	dirs, _ := filepath.Glob(pattern)
	for _, dir := range dirs {
		run, err := Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		files, err := run.Sources()
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		details := map[string]int{}
		texts := map[string]bool{}
		for _, rec := range run.Records {
			if _, ok := run.PURL(rec); !ok {
				counts["no-purl"]++
			}
			r := Resolve(rec, files)
			counts[string(r.Outcome)+" "+r.Reason]++
			if r.Outcome == Unresolved && r.Reason == ReasonDEP5LicenseMissing {
				details[r.Detail]++
			}
			if r.Outcome == Linked {
				texts[string(r.Text)] = true
			}
		}
		t.Logf("%s: %d records, %d distinct texts, %v", filepath.Base(dir), len(run.Records), len(texts), counts)
		var ds []string
		for d := range details {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		for i, d := range ds {
			if i < 15 {
				t.Logf("   missing: %s (%d)", d, details[d])
			}
		}
	}
}
