package hoplites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func hexHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func record(relation string, evidence map[string]any) Record {
	raw, _ := json.Marshal(evidence)
	expr := "MIT"
	return Record{ID: "r1", Relation: relation, DeclaredExpression: &expr, Evidence: raw}
}

func TestResolve(t *testing.T) {
	copyright := []byte("Format: x\n\nFiles: *\nCopyright: 2020 A\nLicense: MIT\n\nLicense: MIT\n Permission is granted.\n")
	notice := []byte("// header\n/* MIT notice */\nint main() {}\n")
	files := Files{
		hexHash(copyright): {Path: "sources/copyright", Data: copyright},
		hexHash(notice):    {Path: "sources/main.c", Data: notice},
	}
	slice := notice[10:26]

	cases := []struct {
		name    string
		rec     Record
		outcome Outcome
		reason  string
		text    string
	}{
		{"dep5", record("source-file-declaration", map[string]any{
			"sha256": hexHash(copyright), "source_label": "MIT", "source_selector": "*", "copyright": "2020 A"}),
			Linked, "", "License: MIT\n Permission is granted.\n"},
		{"document", record("source-document", map[string]any{"sha256": hexHash(copyright)}),
			Linked, "", string(copyright)},
		{"range", record("source-notice", map[string]any{
			"notice_sha256": hexHash(notice), "source_byte_start": 10, "source_byte_end": 26, "text_sha256": hexHash(slice)}),
			Linked, "", string(slice)},
		{"range hash mismatch", record("source-notice", map[string]any{
			"notice_sha256": hexHash(notice), "source_byte_start": 0, "source_byte_end": 9, "text_sha256": hexHash(slice)}),
			Unresolved, ReasonRangeInvalid, ""},
		{"whole source file", record("installed-source", map[string]any{"notice_sha256": hexHash(notice)}),
			Unresolved, ReasonNoticeRangeMissing, ""},
		{"missing file", record("source-document", map[string]any{"sha256": "00"}),
			Unresolved, ReasonSourceMissing, ""},
		{"scanner digest", record("scanner-evidence-reference", nil),
			Skipped, ReasonNotLicenseTerms, ""},
		{"unknown", record("something-new", map[string]any{"sha256": hexHash(copyright)}),
			Unresolved, ReasonUnknownRelation, ""},
	}
	for _, c := range cases {
		r := Resolve(c.rec, files)
		if r.Outcome != c.outcome || r.Reason != c.reason || string(r.Text) != c.text {
			t.Errorf("%s: got %s/%s %q (%s)", c.name, r.Outcome, r.Reason, r.Text, r.Detail)
		}
		if r.SPDX != "MIT" {
			t.Errorf("%s: spdx %q", c.name, r.SPDX)
		}
	}

	r := Resolve(cases[0].rec, files)
	if r.Holders != "2020 A" {
		t.Errorf("holders: %q", r.Holders)
	}
}

func TestStripQualifier(t *testing.T) {
	got := stripQualifier("pkg:deb/debian/a@1?arch=all&package-id=abc&distro=d", "package-id")
	if got != "pkg:deb/debian/a@1?arch=all&distro=d" {
		t.Fatal(got)
	}
	if got := stripQualifier("pkg:generic/p@1?package-id=x", "package-id"); got != "pkg:generic/p@1" {
		t.Fatal(got)
	}
}
