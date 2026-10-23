// Package interpretation models structured readings of a license text.
//
// An interpretation is keyed by the exact hash of the text it reads, so an
// approved interpretation is reused by every component that ships the same
// text. Interpretations are submitted by agents and only become effective
// once a person approves them.
package interpretation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/sensibile/thesmothetai/schemas"
)

// Status is the review state of an interpretation.
type Status string

const (
	StatusPending          Status = "pending"
	StatusReviewRequired   Status = "review_required"
	StatusApproved         Status = "approved"
	StatusRejected         Status = "rejected"
	StatusChangesRequested Status = "changes_requested"
	StatusSuperseded       Status = "superseded"
)

// Open reports whether the interpretation is still awaiting a decision.
func (s Status) Open() bool {
	return s == StatusPending || s == StatusReviewRequired
}

type Obligation struct {
	ID       string   `json:"id"`
	Triggers []string `json:"triggers"`
	Clause   string   `json:"clause"`
	Quote    string   `json:"quote,omitempty"`
}

type Uncertainty struct {
	Clause string `json:"clause"`
	Reason string `json:"reason"`
}

type Provenance struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Skill        string `json:"skill"`
	SkillVersion string `json:"skill_version"`
}

// Submission is the agent-facing contract defined by
// schemas/interpretation.v1.json.
type Submission struct {
	SchemaVersion string        `json:"schema_version"`
	SourceHash    string        `json:"source_hash"`
	License       string        `json:"license"`
	Summary       string        `json:"summary"`
	Obligations   []Obligation  `json:"obligations"`
	Uncertainties []Uncertainty `json:"uncertainties"`
	Provenance    Provenance    `json:"provenance"`
}

// Interpretation is a stored submission and its review state.
type Interpretation struct {
	ID string `json:"id"`
	Submission
	Status      Status    `json:"status"`
	ReviewNote  string    `json:"review_note,omitempty"`
	SubmittedAt time.Time `json:"submitted_at"`
}

var compileSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemas.InterpretationV1))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemas.InterpretationV1ID, doc); err != nil {
		return nil, err
	}
	return c.Compile(schemas.InterpretationV1ID)
})

// Parse validates data against the v1 schema and decodes it.
func Parse(data []byte) (Submission, error) {
	sch, err := compileSchema()
	if err != nil {
		return Submission{}, fmt.Errorf("compile schema: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return Submission{}, fmt.Errorf("decode submission: %w", err)
	}
	if err := sch.Validate(inst); err != nil {
		return Submission{}, fmt.Errorf("submission does not match schema: %w", err)
	}
	var s Submission
	if err := json.Unmarshal(data, &s); err != nil {
		return Submission{}, fmt.Errorf("decode submission: %w", err)
	}
	return s, nil
}

// Compare reports the substantive differences between two interpretations
// of the same text. Wording of summaries, clauses and quotes is ignored;
// the license identifier and the set of (obligation, triggers) pairs must
// match. An empty result means the interpretations agree.
func Compare(a, b Submission) []string {
	var diffs []string
	if a.License != b.License {
		diffs = append(diffs, fmt.Sprintf("license: %s vs %s", a.License, b.License))
	}
	ka, kb := obligationKeys(a), obligationKeys(b)
	for _, k := range ka {
		if !slices.Contains(kb, k) {
			diffs = append(diffs, "only in "+a.Provenance.Provider+": "+k)
		}
	}
	for _, k := range kb {
		if !slices.Contains(ka, k) {
			diffs = append(diffs, "only in "+b.Provenance.Provider+": "+k)
		}
	}
	return diffs
}

func obligationKeys(s Submission) []string {
	keys := make([]string, 0, len(s.Obligations))
	for _, o := range s.Obligations {
		triggers := slices.Clone(o.Triggers)
		slices.Sort(triggers)
		keys = append(keys, fmt.Sprintf("%s when [%s]", o.ID, strings.Join(triggers, ", ")))
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}
