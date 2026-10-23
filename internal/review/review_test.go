package review

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sensibile/thesmothetai/internal/interpretation"
	"github.com/sensibile/thesmothetai/internal/store"
)

// drive feeds msg to m and runs any resulting commands synchronously,
// ignoring the cursor-blink and quit commands the UI emits.
func drive(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(model)
	if cmd == nil {
		return m
	}
	switch out := cmd().(type) {
	case listMsg, detailMsg, decidedMsg, errMsg:
		return drive(t, m, out)
	}
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestApproveRequiresAcknowledgementAndReason(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ev, err := st.AddEvidence(ctx, store.EvidenceInput{PURL: "pkg:npm/x@1.0.0", SPDXExpression: "MIT", Text: []byte("MIT text\n")})
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Submit(ctx, interpretation.Submission{
		SchemaVersion: "1",
		SourceHash:    ev.Evidence.TextHash,
		License:       "MIT",
		Summary:       "Permissive.",
		Uncertainties: []interpretation.Uncertainty{{Clause: "2", Reason: "unclear"}},
		Provenance:    interpretation.Provenance{Provider: "claude", Model: "m", Skill: "s", SkillVersion: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(ctx, st, "tester")
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(t, m, m.Init()())
	m = drive(t, m, key("enter"))
	if m.mode != modeDetail {
		t.Fatalf("expected detail mode, got %v", m.mode)
	}
	for range tabCount {
		m = drive(t, m, key("tab"))
		_ = m.View()
	}

	m = drive(t, m, key("a"))
	if m.mode != modeDetail || !strings.Contains(m.flash, "uncertainties") {
		t.Fatalf("approval without acknowledgement should be refused, flash=%q", m.flash)
	}

	m = drive(t, m, key("u"))
	m = drive(t, m, key("a"))
	if m.mode != modeReason {
		t.Fatalf("expected reason prompt, got %v", m.mode)
	}
	m = drive(t, m, key("enter"))
	if m.mode != modeReason || !strings.Contains(m.flash, "reason") {
		t.Fatalf("empty reason should be refused, flash=%q", m.flash)
	}

	m = drive(t, m, key("matches MIT"))
	m = drive(t, m, key("enter"))
	if m.mode != modeList || len(m.items) != 0 {
		t.Fatalf("expected empty list after approval, mode=%v items=%d flash=%q", m.mode, len(m.items), m.flash)
	}

	got, _ := st.Interpretation(ctx, it.ID)
	ds, _ := st.Decisions(ctx, it.ID)
	if got.Status != interpretation.StatusApproved || len(ds) != 1 || ds[0].Actor != "tester" ||
		ds[0].Reason != "matches MIT" || !ds[0].UncertaintiesAcknowledged {
		t.Fatalf("status=%s decisions=%+v", got.Status, ds)
	}
}
