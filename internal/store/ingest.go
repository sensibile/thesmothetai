package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sensibile/thesmothetai/internal/evidence"
)

var ErrAlreadyIngested = errors.New("this input was already ingested")

// Scope record outcomes, mirroring the collector's.
const (
	OutcomeLinked     = "linked"
	OutcomeSkipped    = "skipped"
	OutcomeUnresolved = "unresolved"
)

// ScopeInput is one upstream scope record. Text is set only for linked
// records.
type ScopeInput struct {
	RecordID              string
	Relation              string
	Subject               string
	OwnerRef              string
	PURL                  string
	DeclaredExpression    string
	Inclusion             string
	ObservedApplicability string
	Outcome               string
	Reason                string
	Detail                string
	Text                  []byte
	TextOrigin            string
	Holders               string
}

type IngestInput struct {
	Source      string
	RunDir      string
	ArtifactRef string
	CodeCommit  string
	RuleVersion string
	InputSHA256 string
	Records     []ScopeInput
}

type IngestResult struct {
	RunID       string         `json:"run_id"`
	ArtifactRef string         `json:"artifact_ref"`
	Records     int            `json:"records"`
	Outcomes    map[string]int `json:"outcomes"`
	// Reasons counts skipped and unresolved records by reason.
	Reasons map[string]int `json:"reasons"`
	Texts   TextSummary    `json:"texts"`
}

// TextSummary describes the distinct license texts the run linked.
type TextSummary struct {
	Distinct int `json:"distinct"`
	// Reused texts already have an approved interpretation.
	Reused int `json:"reused"`
	// New texts were not in the registry before this run.
	New int `json:"new"`
}

// Ingest records a collector run in one transaction. Re-ingesting the
// same input returns ErrAlreadyIngested and the earlier run id.
func (s *Store) Ingest(ctx context.Context, in IngestInput) (IngestResult, error) {
	res := IngestResult{
		RunID:       newID("run"),
		ArtifactRef: in.ArtifactRef,
		Records:     len(in.Records),
		Outcomes:    map[string]int{},
		Reasons:     map[string]int{},
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM ingest_runs WHERE input_sha256 = ?`, in.InputSHA256).Scan(&existing)
		if err == nil {
			res.RunID = existing
			return fmt.Errorf("%w as %s", ErrAlreadyIngested, existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ingest_runs (id, source, run_dir, artifact_ref, code_commit, rule_version, input_sha256, ingested_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			res.RunID, in.Source, in.RunDir, in.ArtifactRef, in.CodeCommit, in.RuleVersion, in.InputSHA256,
			formatTime(s.now())); err != nil {
			return err
		}

		known := map[string]bool{}
		for _, rec := range in.Records {
			outcome, reason, err := s.ingestRecord(ctx, tx, res.RunID, in.Source, rec, known)
			if err != nil {
				return fmt.Errorf("record %s: %w", rec.RecordID, err)
			}
			res.Outcomes[outcome]++
			if outcome != OutcomeLinked {
				res.Reasons[reason]++
			}
		}

		res.Texts.Distinct = len(known)
		for hash, isNew := range known {
			if isNew {
				res.Texts.New++
			}
			approved, err := approvedFor(ctx, tx, hash)
			if err != nil {
				return err
			}
			if approved != nil {
				res.Texts.Reused++
			}
		}
		return nil
	})
	return res, err
}

// ingestRecord stores rec and returns its final outcome; a linked record
// whose owner has no usable purl is downgraded to unresolved.
func (s *Store) ingestRecord(ctx context.Context, tx *sql.Tx, runID, source string, rec ScopeInput, known map[string]bool) (outcome, reason string, err error) {
	var compID, evidenceID any
	if rec.PURL != "" {
		comp, err := evidence.ParsePURL(rec.PURL)
		if err != nil {
			rec.Outcome, rec.Reason, rec.Detail, rec.Text = OutcomeUnresolved, "owner-purl-invalid", err.Error(), nil
		} else {
			id, err := upsertComponent(ctx, tx, comp)
			if err != nil {
				return "", "", err
			}
			compID = id
			if rec.Outcome == OutcomeLinked {
				id, err := s.linkText(ctx, tx, comp, source, rec, known)
				if err != nil {
					return "", "", err
				}
				evidenceID = id
			}
		}
	} else if rec.Outcome == OutcomeLinked {
		rec.Outcome, rec.Reason, rec.Text = OutcomeUnresolved, "owner-purl-missing", nil
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO scope_records (run_id, record_id, relation, subject, owner_ref, component_id, declared_expression,
			inclusion, observed_applicability, evidence_id, text_origin, holders, outcome, reason, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, rec.RecordID, rec.Relation, rec.Subject, rec.OwnerRef, compID, rec.DeclaredExpression,
		rec.Inclusion, rec.ObservedApplicability, evidenceID, rec.TextOrigin, rec.Holders,
		rec.Outcome, rec.Reason, rec.Detail)
	return rec.Outcome, rec.Reason, err
}

// linkText records the text as evidence and returns the evidence id.
// known maps each text hash linked in this run to whether it was new.
func (s *Store) linkText(ctx context.Context, tx *sql.Tx, comp evidence.Component, source string, rec ScopeInput, known map[string]bool) (string, error) {
	ev, _, textNew, err := s.insertEvidence(ctx, tx, comp, EvidenceInput{
		PURL:           rec.PURL,
		SPDXExpression: rec.DeclaredExpression,
		Text:           rec.Text,
		Source:         source + ":" + rec.Relation,
	})
	if err != nil {
		return "", err
	}
	if _, seen := known[ev.TextHash]; !seen {
		known[ev.TextHash] = textNew
	}
	return ev.ID, nil
}

// ScopeRecord is a stored upstream record, for reporting.
type ScopeRecord struct {
	RecordID   string `json:"record_id"`
	Relation   string `json:"relation"`
	Subject    string `json:"subject"`
	OwnerRef   string `json:"owner_ref"`
	Declared   string `json:"declared_expression"`
	Inclusion  string `json:"inclusion"`
	EvidenceID string `json:"evidence_id,omitempty"`
	TextOrigin string `json:"text_origin,omitempty"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// ScopeRecords lists a run's records, optionally filtered by outcome.
func (s *Store) ScopeRecords(ctx context.Context, runID, outcome string) ([]ScopeRecord, error) {
	q := `SELECT record_id, relation, subject, owner_ref, declared_expression, inclusion,
		coalesce(evidence_id, ''), text_origin, outcome, reason, detail
		FROM scope_records WHERE run_id = ?`
	args := []any{runID}
	if outcome != "" {
		q += ` AND outcome = ?`
		args = append(args, outcome)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY owner_ref, record_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScopeRecord{}
	for rows.Next() {
		var r ScopeRecord
		if err := rows.Scan(&r.RecordID, &r.Relation, &r.Subject, &r.OwnerRef, &r.Declared, &r.Inclusion,
			&r.EvidenceID, &r.TextOrigin, &r.Outcome, &r.Reason, &r.Detail); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
