package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sensibile/thesmothetai/internal/decision"
	"github.com/sensibile/thesmothetai/internal/interpretation"
)

const mitText = "MIT License\n\nPermission is hereby granted, free of charge...\n"

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func submission(hash, provider string, obligations ...string) interpretation.Submission {
	sub := interpretation.Submission{
		SchemaVersion: "1",
		SourceHash:    hash,
		License:       "MIT",
		Summary:       "Permissive.",
		Uncertainties: []interpretation.Uncertainty{},
		Provenance:    interpretation.Provenance{Provider: provider, Model: "m", Skill: "license-interpret", SkillVersion: "1"},
	}
	for _, o := range obligations {
		sub.Obligations = append(sub.Obligations, interpretation.Obligation{ID: o, Triggers: []string{"distribution"}, Clause: "para 2"})
	}
	return sub
}

func TestReuseAcrossVersions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	first, err := s.AddEvidence(ctx, EvidenceInput{PURL: "pkg:npm/left-pad@1.0.0", SPDXExpression: "MIT", Text: []byte(mitText)})
	if err != nil {
		t.Fatal(err)
	}
	if first.Interpretation != "required" || first.PreviousVersion != nil {
		t.Fatalf("first evidence: %+v", first)
	}
	hash := first.Evidence.TextHash

	q, err := s.Queue(ctx, "")
	if err != nil || len(q) != 1 || q[0].SourceHash != hash {
		t.Fatalf("queue: %+v %v", q, err)
	}

	claude, err := s.Submit(ctx, submission(hash, "claude", "include_license_text"))
	if err != nil {
		t.Fatal(err)
	}
	if q, _ := s.Queue(ctx, "claude"); len(q) != 0 {
		t.Fatalf("claude already submitted, queue should skip: %+v", q)
	}
	codex, err := s.Submit(ctx, submission(hash, "codex", "include_license_text"))
	if err != nil {
		t.Fatal(err)
	}
	if codex.Status != interpretation.StatusPending {
		t.Fatalf("agreeing submissions should stay pending, got %s (%s)", codex.Status, codex.ReviewNote)
	}

	if _, err := s.Decide(ctx, decision.Request{InterpretationID: claude.ID, Action: decision.ActionApprove, Reason: "ok", Actor: "me"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Interpretation(ctx, codex.ID); got.Status != interpretation.StatusSuperseded {
		t.Fatalf("codex submission should be superseded, got %s", got.Status)
	}
	if q, _ := s.Queue(ctx, ""); len(q) != 0 {
		t.Fatalf("approved text should leave the queue: %+v", q)
	}
	if _, err := s.Submit(ctx, submission(hash, "claude")); !errors.Is(err, ErrAlreadyApproved) {
		t.Fatalf("expected ErrAlreadyApproved, got %v", err)
	}

	next, err := s.AddEvidence(ctx, EvidenceInput{PURL: "pkg:npm/left-pad@1.1.0", SPDXExpression: "MIT", Text: []byte(mitText)})
	if err != nil {
		t.Fatal(err)
	}
	if next.Interpretation != "reused" || next.ReusedInterpretation != claude.ID {
		t.Fatalf("same text should reuse approval: %+v", next)
	}
	if next.PreviousVersion == nil || next.PreviousVersion.TextChanged {
		t.Fatalf("previous version should be detected as unchanged: %+v", next.PreviousVersion)
	}

	changed, err := s.AddEvidence(ctx, EvidenceInput{PURL: "pkg:npm/left-pad@2.0.0", SPDXExpression: "MIT", Text: []byte(mitText + "Extra clause.\n")})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Interpretation != "required" || !changed.PreviousVersion.TextChanged {
		t.Fatalf("changed text must require interpretation: %+v", changed)
	}
	ic, err := s.InterpretContext(ctx, changed.Evidence.TextHash)
	if err != nil {
		t.Fatal(err)
	}
	if ic.Previous == nil || ic.Previous.Interpretation.ID != claude.ID {
		t.Fatalf("context should point at the previous approval: %+v", ic.Previous)
	}
}

func TestDisagreementRequiresReview(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	ev, err := s.AddEvidence(ctx, EvidenceInput{PURL: "pkg:golang/example.com/x@v1.0.0", SPDXExpression: "MIT", Text: []byte(mitText)})
	if err != nil {
		t.Fatal(err)
	}
	hash := ev.Evidence.TextHash

	a, _ := s.Submit(ctx, submission(hash, "claude", "include_license_text"))
	b, err := s.Submit(ctx, submission(hash, "codex", "include_license_text", "preserve_copyright_notice"))
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != interpretation.StatusReviewRequired {
		t.Fatalf("got %s", b.Status)
	}
	if got, _ := s.Interpretation(ctx, a.ID); got.Status != interpretation.StatusReviewRequired {
		t.Fatalf("earlier submission should also need review, got %s", got.Status)
	}

	list, _ := s.Interpretations(ctx, interpretation.StatusPending, interpretation.StatusReviewRequired)
	if len(list) != 2 {
		t.Fatalf("expected 2 open, got %d", len(list))
	}
}

func TestSubmitUnknownHash(t *testing.T) {
	s := openTest(t)
	_, err := s.Submit(context.Background(), submission("sha256:unknown", "claude"))
	if !errors.Is(err, ErrUnknownSourceHash) {
		t.Fatalf("got %v", err)
	}
}
