package store

import (
	"context"
	"errors"
	"testing"
)

func TestIngest(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	in := IngestInput{
		Source: "hoplites", RunDir: "/runs/a", ArtifactRef: "img@sha256:1", RuleVersion: "v1", InputSHA256: "sha256:aa",
		Records: []ScopeInput{
			{RecordID: "1", PURL: "pkg:deb/debian/a@1", DeclaredExpression: "MIT", Outcome: OutcomeLinked, Text: []byte(mitText)},
			{RecordID: "2", PURL: "pkg:deb/debian/b@2", DeclaredExpression: "MIT", Outcome: OutcomeLinked, Text: []byte(mitText)},
			{RecordID: "3", PURL: "pkg:deb/debian/b@2", Outcome: OutcomeSkipped, Reason: "not-license-terms"},
			{RecordID: "4", PURL: "", DeclaredExpression: "MIT", Outcome: OutcomeLinked, Text: []byte("x")},
		},
	}
	res, err := s.Ingest(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[OutcomeLinked] != 2 || res.Outcomes[OutcomeUnresolved] != 1 || res.Reasons["owner-purl-missing"] != 1 {
		t.Fatalf("outcomes %+v reasons %+v", res.Outcomes, res.Reasons)
	}
	if res.Texts.Distinct != 1 || res.Texts.New != 1 {
		t.Fatalf("texts %+v", res.Texts)
	}
	if q, _ := s.Queue(ctx, ""); len(q) != 1 || len(q[0].Components) != 2 {
		t.Fatalf("one shared text should be queued once: %+v", q)
	}

	recs, _ := s.ScopeRecords(ctx, res.RunID, OutcomeUnresolved)
	if len(recs) != 1 || recs[0].RecordID != "4" {
		t.Fatalf("unresolved records: %+v", recs)
	}

	if _, err := s.Ingest(ctx, in); !errors.Is(err, ErrAlreadyIngested) {
		t.Fatalf("expected ErrAlreadyIngested, got %v", err)
	}

	in.InputSHA256, in.RunDir = "sha256:bb", "/runs/b"
	again, err := s.Ingest(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again.Texts.New != 0 {
		t.Fatalf("text seen in an earlier run is not new: %+v", again.Texts)
	}
}
