package interpretation

import (
	"strings"
	"testing"
)

const validSubmission = `{
  "schema_version": "1",
  "source_hash": "sha256:` + "0000000000000000000000000000000000000000000000000000000000000000" + `",
  "license": "MIT",
  "summary": "Permissive license.",
  "obligations": [
    {"id": "include_license_text", "triggers": ["distribution"], "clause": "para 2"}
  ],
  "uncertainties": [],
  "provenance": {"provider": "claude", "model": "claude-opus-5-5", "skill": "license-interpret", "skill_version": "1"}
}`

func TestParseValid(t *testing.T) {
	s, err := Parse([]byte(validSubmission))
	if err != nil {
		t.Fatal(err)
	}
	if s.License != "MIT" || len(s.Obligations) != 1 {
		t.Fatalf("got %+v", s)
	}
}

func TestParseRejectsSchemaViolations(t *testing.T) {
	cases := map[string]string{
		"unknown field": strings.Replace(validSubmission, `"license"`, `"extra": 1, "license"`, 1),
		"bad hash":      strings.Replace(validSubmission, "sha256:0", "md5:0", 1),
		"bad provider":  strings.Replace(validSubmission, `"provider": "claude"`, `"provider": "gpt"`, 1),
		"bad id":        strings.Replace(validSubmission, `"include_license_text"`, `"Include License"`, 1),
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected schema error", name)
		}
	}
}

func TestCompare(t *testing.T) {
	a, _ := Parse([]byte(validSubmission))
	b := a
	b.Provenance.Provider = "codex"
	b.Summary = "Different wording."
	b.Obligations = []Obligation{{ID: "include_license_text", Triggers: []string{"distribution"}, Clause: "second paragraph"}}
	if d := Compare(a, b); len(d) != 0 {
		t.Fatalf("expected agreement, got %v", d)
	}

	b.Obligations = append(b.Obligations, Obligation{ID: "preserve_copyright_notice", Clause: "para 2"})
	d := Compare(a, b)
	if len(d) != 1 || !strings.Contains(d[0], "only in codex: preserve_copyright_notice") {
		t.Fatalf("got %v", d)
	}
}
