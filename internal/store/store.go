// Package store persists the decision registry in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sensibile/thesmothetai/internal/decision"
	"github.com/sensibile/thesmothetai/internal/evidence"
	"github.com/sensibile/thesmothetai/internal/fingerprint"
	"github.com/sensibile/thesmothetai/internal/interpretation"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrUnknownSourceHash = errors.New("source_hash does not match any collected license text")
	ErrAlreadyApproved   = errors.New("an approved interpretation already exists for this text")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies
// pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func newID(prefix string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// --- Evidence ---------------------------------------------------------------

type EvidenceInput struct {
	PURL           string
	SPDXExpression string
	Text           []byte
	Source         string
}

// VersionChange compares new evidence with the latest evidence collected
// for another version of the same package.
type VersionChange struct {
	PURL           string `json:"purl"`
	SPDXExpression string `json:"spdx_expression"`
	TextHash       string `json:"text_hash"`
	SPDXChanged    bool   `json:"spdx_changed"`
	TextChanged    bool   `json:"text_changed"`
	// FormattingOnly is informational: the text still needs its own
	// interpretation because the exact hash differs.
	FormattingOnly bool `json:"formatting_only,omitempty"`
}

type AddEvidenceResult struct {
	Evidence evidence.Evidence `json:"evidence"`
	Created  bool              `json:"created"`
	// Interpretation is "reused" when an approved interpretation of the
	// exact same text exists, otherwise "required".
	Interpretation       string         `json:"interpretation"`
	ReusedInterpretation string         `json:"reused_interpretation,omitempty"`
	PreviousVersion      *VersionChange `json:"previous_version,omitempty"`
}

func (s *Store) AddEvidence(ctx context.Context, in EvidenceInput) (AddEvidenceResult, error) {
	comp, err := evidence.ParsePURL(in.PURL)
	if err != nil {
		return AddEvidenceResult{}, err
	}
	if strings.TrimSpace(in.SPDXExpression) == "" {
		return AddEvidenceResult{}, errors.New("spdx expression is required")
	}
	if len(in.Text) == 0 {
		return AddEvidenceResult{}, errors.New("license text is empty")
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	hash := fingerprint.Hash(in.Text)
	norm := fingerprint.NormalizedHash(in.Text)

	var res AddEvidenceResult
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if res.Evidence, res.Created, _, err = s.insertEvidence(ctx, tx, comp, in); err != nil {
			return err
		}
		comp = res.Evidence.Component

		var prev VersionChange
		var prevNorm string
		err = tx.QueryRowContext(ctx, `
			SELECT c.purl, e.spdx_expression, e.text_hash, t.normalized_hash
			FROM evidence e
			JOIN components c ON c.id = e.component_id
			JOIN license_texts t ON t.hash = e.text_hash
			WHERE c.package = ? AND c.id <> ?
			ORDER BY e.collected_at DESC, e.rowid DESC LIMIT 1`,
			comp.Package, comp.ID).Scan(&prev.PURL, &prev.SPDXExpression, &prev.TextHash, &prevNorm)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		default:
			prev.SPDXChanged = prev.SPDXExpression != in.SPDXExpression
			prev.TextChanged = prev.TextHash != hash
			prev.FormattingOnly = prev.TextChanged && prevNorm == norm
			res.PreviousVersion = &prev
		}

		approved, err := approvedFor(ctx, tx, hash)
		if err != nil {
			return err
		}
		res.Interpretation = "required"
		if approved != nil {
			res.Interpretation = "reused"
			res.ReusedInterpretation = approved.ID
		}
		return nil
	})
	return res, err
}

func upsertComponent(ctx context.Context, tx *sql.Tx, comp evidence.Component) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO components (purl, package, version) VALUES (?, ?, ?) ON CONFLICT (purl) DO NOTHING`,
		comp.PURL, comp.Package, comp.Version); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM components WHERE purl = ?`, comp.PURL).Scan(&id)
	return id, err
}

// insertEvidence records in for comp, returning the existing row when the
// same component, expression and text were already collected. textNew
// reports whether the license text itself was first seen here.
func (s *Store) insertEvidence(ctx context.Context, tx *sql.Tx, comp evidence.Component, in EvidenceInput) (ev evidence.Evidence, created, textNew bool, err error) {
	compID, err := upsertComponent(ctx, tx, comp)
	if err != nil {
		return ev, false, false, err
	}
	hash := fingerprint.Hash(in.Text)
	r, err := tx.ExecContext(ctx,
		`INSERT INTO license_texts (hash, normalized_hash, text) VALUES (?, ?, ?) ON CONFLICT (hash) DO NOTHING`,
		hash, fingerprint.NormalizedHash(in.Text), string(in.Text))
	if err != nil {
		return ev, false, false, err
	}
	n, _ := r.RowsAffected()
	textNew = n == 1

	r, err = tx.ExecContext(ctx,
		`INSERT INTO evidence (id, component_id, spdx_expression, text_hash, source, collected_at)
		 VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		newID("ev"), compID, in.SPDXExpression, hash, in.Source, formatTime(s.now()))
	if err != nil {
		return ev, false, false, err
	}
	n, _ = r.RowsAffected()

	var id string
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM evidence WHERE component_id = ? AND spdx_expression = ? AND text_hash = ?`,
		compID, in.SPDXExpression, hash).Scan(&id); err != nil {
		return ev, false, false, err
	}
	ev, err = getEvidence(ctx, tx, id)
	return ev, n == 1, textNew, err
}

const evidenceColumns = `e.id, e.spdx_expression, e.text_hash, t.normalized_hash, e.source, e.collected_at,
	c.id, c.purl, c.package, c.version
	FROM evidence e
	JOIN components c ON c.id = e.component_id
	JOIN license_texts t ON t.hash = e.text_hash`

func scanEvidence(row interface{ Scan(...any) error }) (evidence.Evidence, error) {
	var e evidence.Evidence
	var collected string
	err := row.Scan(&e.ID, &e.SPDXExpression, &e.TextHash, &e.NormalizedHash, &e.Source, &collected,
		&e.Component.ID, &e.Component.PURL, &e.Component.Package, &e.Component.Version)
	e.CollectedAt = parseTime(collected)
	return e, err
}

func getEvidence(ctx context.Context, q querier, id string) (evidence.Evidence, error) {
	e, err := scanEvidence(q.QueryRowContext(ctx, `SELECT `+evidenceColumns+` WHERE e.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("evidence %s: %w", id, ErrNotFound)
	}
	return e, err
}

func (s *Store) Evidence(ctx context.Context, id string) (evidence.Evidence, error) {
	return getEvidence(ctx, s.db, id)
}

// EvidenceByHash lists every evidence row that carries the given text.
func (s *Store) EvidenceByHash(ctx context.Context, hash string) ([]evidence.Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+evidenceColumns+` WHERE e.text_hash = ? ORDER BY c.purl`, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []evidence.Evidence
	for rows.Next() {
		e, err := scanEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) LicenseText(ctx context.Context, hash string) (string, error) {
	var text string
	err := s.db.QueryRowContext(ctx, `SELECT text FROM license_texts WHERE hash = ?`, hash).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("license text %s: %w", hash, ErrNotFound)
	}
	return text, err
}

// --- Interpretation queue ---------------------------------------------------

// QueueItem is a license text that has no approved interpretation yet.
type QueueItem struct {
	SourceHash      string   `json:"source_hash"`
	SPDXExpressions []string `json:"spdx_expressions"`
	Components      []string `json:"components"`
	EvidenceIDs     []string `json:"evidence_ids"`
	// OpenSubmissions lists providers whose interpretation awaits review.
	OpenSubmissions []string `json:"open_submissions"`
}

// Queue lists texts that still need interpretation. When provider is set,
// texts that provider already has an open submission for are skipped.
func (s *Store) Queue(ctx context.Context, provider string) ([]QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.text_hash, e.id, e.spdx_expression, c.purl
		FROM evidence e JOIN components c ON c.id = e.component_id
		WHERE NOT EXISTS (
			SELECT 1 FROM interpretations i WHERE i.text_hash = e.text_hash AND i.status = 'approved')
		ORDER BY e.collected_at, e.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var order []string
	items := map[string]*QueueItem{}
	for rows.Next() {
		var hash, id, spdx, purl string
		if err := rows.Scan(&hash, &id, &spdx, &purl); err != nil {
			return nil, err
		}
		it, ok := items[hash]
		if !ok {
			it = &QueueItem{SourceHash: hash, SPDXExpressions: []string{}, OpenSubmissions: []string{}}
			items[hash] = it
			order = append(order, hash)
		}
		it.EvidenceIDs = append(it.EvidenceIDs, id)
		it.Components = append(it.Components, purl)
		if !slices.Contains(it.SPDXExpressions, spdx) {
			it.SPDXExpressions = append(it.SPDXExpressions, spdx)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	open, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT text_hash, provider FROM interpretations WHERE status IN ('pending', 'review_required')`)
	if err != nil {
		return nil, err
	}
	defer open.Close()
	for open.Next() {
		var hash, p string
		if err := open.Scan(&hash, &p); err != nil {
			return nil, err
		}
		if it, ok := items[hash]; ok {
			it.OpenSubmissions = append(it.OpenSubmissions, p)
		}
	}
	if err := open.Err(); err != nil {
		return nil, err
	}

	out := []QueueItem{}
	for _, h := range order {
		it := items[h]
		if provider != "" && slices.Contains(it.OpenSubmissions, provider) {
			continue
		}
		out = append(out, *it)
	}
	return out, nil
}

// PreviousApproved is the approved interpretation of the text another
// version of the same package shipped.
type PreviousApproved struct {
	SourceHash     string                        `json:"source_hash"`
	PURL           string                        `json:"purl"`
	Text           string                        `json:"text"`
	Interpretation interpretation.Interpretation `json:"interpretation"`
}

// InterpretContext is everything an interpreter needs for one text.
type InterpretContext struct {
	SourceHash string              `json:"source_hash"`
	Text       string              `json:"text"`
	Evidence   []evidence.Evidence `json:"evidence"`
	Previous   *PreviousApproved   `json:"previous_approved,omitempty"`
}

func (s *Store) InterpretContext(ctx context.Context, hash string) (InterpretContext, error) {
	text, err := s.LicenseText(ctx, hash)
	if err != nil {
		return InterpretContext{}, err
	}
	ev, err := s.EvidenceByHash(ctx, hash)
	if err != nil {
		return InterpretContext{}, err
	}
	prev, err := s.PreviousApproved(ctx, hash)
	if err != nil {
		return InterpretContext{}, err
	}
	return InterpretContext{SourceHash: hash, Text: text, Evidence: ev, Previous: prev}, nil
}

// PreviousApproved finds the most recent approved interpretation of a
// different text shipped by another version of a package that ships hash.
// It returns nil when there is none.
func (s *Store) PreviousApproved(ctx context.Context, hash string) (*PreviousApproved, error) {
	var p PreviousApproved
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT e2.text_hash, c2.purl, i.id
		FROM evidence e
		JOIN components c  ON c.id = e.component_id
		JOIN components c2 ON c2.package = c.package AND c2.id <> c.id
		JOIN evidence e2   ON e2.component_id = c2.id AND e2.text_hash <> e.text_hash
		JOIN interpretations i ON i.text_hash = e2.text_hash AND i.status = 'approved'
		WHERE e.text_hash = ?
		ORDER BY e2.collected_at DESC LIMIT 1`, hash).Scan(&p.SourceHash, &p.PURL, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.Text, err = s.LicenseText(ctx, p.SourceHash); err != nil {
		return nil, err
	}
	if p.Interpretation, err = s.Interpretation(ctx, id); err != nil {
		return nil, err
	}
	return &p, nil
}

// --- Interpretations --------------------------------------------------------

const interpretationColumns = `id, body, status, review_note, submitted_at FROM interpretations`

func scanInterpretation(row interface{ Scan(...any) error }) (interpretation.Interpretation, error) {
	var it interpretation.Interpretation
	var body, submitted string
	if err := row.Scan(&it.ID, &body, &it.Status, &it.ReviewNote, &submitted); err != nil {
		return it, err
	}
	it.SubmittedAt = parseTime(submitted)
	if err := json.Unmarshal([]byte(body), &it.Submission); err != nil {
		return it, fmt.Errorf("interpretation %s: decode body: %w", it.ID, err)
	}
	return it, nil
}

func queryInterpretations(ctx context.Context, q querier, where string, args ...any) ([]interpretation.Interpretation, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+interpretationColumns+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []interpretation.Interpretation{}
	for rows.Next() {
		it, err := scanInterpretation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func getInterpretation(ctx context.Context, q querier, id string) (interpretation.Interpretation, error) {
	it, err := scanInterpretation(q.QueryRowContext(ctx, `SELECT `+interpretationColumns+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, fmt.Errorf("interpretation %s: %w", id, ErrNotFound)
	}
	return it, err
}

func approvedFor(ctx context.Context, q querier, hash string) (*interpretation.Interpretation, error) {
	list, err := queryInterpretations(ctx, q, `text_hash = ? AND status = 'approved' ORDER BY submitted_at DESC LIMIT 1`, hash)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (s *Store) Interpretation(ctx context.Context, id string) (interpretation.Interpretation, error) {
	return getInterpretation(ctx, s.db, id)
}

// Interpretations lists interpretations with any of the given statuses,
// or all of them when none are given. Disputed ones come first.
func (s *Store) Interpretations(ctx context.Context, statuses ...interpretation.Status) ([]interpretation.Interpretation, error) {
	where := "1 = 1"
	args := []any{}
	if len(statuses) > 0 {
		where = "status IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(statuses)), ", ") + ")"
		for _, st := range statuses {
			args = append(args, string(st))
		}
	}
	return queryInterpretations(ctx, s.db,
		where+` ORDER BY CASE status WHEN 'review_required' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END, submitted_at`,
		args...)
}

// Submit records an agent's interpretation. It is cross-checked against
// open submissions from other providers for the same text: any
// disagreement marks all parties review_required. A provider's earlier
// open submission for the same text is superseded.
func (s *Store) Submit(ctx context.Context, sub interpretation.Submission) (interpretation.Interpretation, error) {
	it := interpretation.Interpretation{
		ID:          newID("int"),
		Submission:  sub,
		Status:      interpretation.StatusPending,
		SubmittedAt: s.now(),
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM license_texts WHERE hash = ?`, sub.SourceHash).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrUnknownSourceHash
		}
		if approved, err := approvedFor(ctx, tx, sub.SourceHash); err != nil {
			return err
		} else if approved != nil {
			return fmt.Errorf("%w (%s)", ErrAlreadyApproved, approved.ID)
		}

		open, err := queryInterpretations(ctx, tx,
			`text_hash = ? AND status IN ('pending', 'review_required') ORDER BY submitted_at`, sub.SourceHash)
		if err != nil {
			return err
		}
		var notes []string
		for _, o := range open {
			if o.Provenance.Provider == sub.Provenance.Provider {
				if err := setStatus(ctx, tx, o.ID, interpretation.StatusSuperseded, "replaced by "+it.ID); err != nil {
					return err
				}
				continue
			}
			diffs := interpretation.Compare(o.Submission, sub)
			if len(diffs) == 0 {
				notes = append(notes, fmt.Sprintf("agrees with %s (%s)", o.ID, o.Provenance.Provider))
				continue
			}
			it.Status = interpretation.StatusReviewRequired
			detail := strings.Join(diffs, "; ")
			notes = append(notes, fmt.Sprintf("disagrees with %s (%s): %s", o.ID, o.Provenance.Provider, detail))
			if err := setStatus(ctx, tx, o.ID, interpretation.StatusReviewRequired,
				fmt.Sprintf("disagrees with %s (%s): %s", it.ID, sub.Provenance.Provider, detail)); err != nil {
				return err
			}
		}
		it.ReviewNote = strings.Join(notes, "\n")

		body, err := json.Marshal(sub)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO interpretations
				(id, text_hash, license, provider, model, skill, skill_version, body, status, review_note, submitted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			it.ID, sub.SourceHash, sub.License, sub.Provenance.Provider, sub.Provenance.Model,
			sub.Provenance.Skill, sub.Provenance.SkillVersion, string(body), string(it.Status), it.ReviewNote,
			formatTime(it.SubmittedAt))
		return err
	})
	return it, err
}

func setStatus(ctx context.Context, q querier, id string, st interpretation.Status, note string) error {
	_, err := q.ExecContext(ctx, `UPDATE interpretations SET status = ?, review_note = ? WHERE id = ?`, string(st), note, id)
	return err
}

// --- Decisions --------------------------------------------------------------

// Decide records a person's verdict. Approving an interpretation
// supersedes every other open interpretation of the same text.
func (s *Store) Decide(ctx context.Context, req decision.Request) (decision.Decision, error) {
	d := decision.Decision{Request: req, DecidedAt: s.now()}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		it, err := getInterpretation(ctx, tx, req.InterpretationID)
		if err != nil {
			return err
		}
		if err := decision.Validate(it, req); err != nil {
			return err
		}
		st, _ := req.Action.ResultStatus()
		r, err := tx.ExecContext(ctx, `
			INSERT INTO decisions (interpretation_id, action, reason, actor, uncertainties_acknowledged, decided_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			req.InterpretationID, string(req.Action), req.Reason, req.Actor, req.UncertaintiesAcknowledged,
			formatTime(d.DecidedAt))
		if err != nil {
			return err
		}
		if d.ID, err = r.LastInsertId(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE interpretations SET status = ? WHERE id = ?`, string(st), it.ID); err != nil {
			return err
		}
		if req.Action != decision.ActionApprove {
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE interpretations SET status = 'superseded', review_note = ?
			WHERE text_hash = ? AND id <> ? AND status IN ('pending', 'review_required')`,
			"superseded by approval of "+it.ID, it.SourceHash, it.ID)
		return err
	})
	return d, err
}

func (s *Store) Decisions(ctx context.Context, interpretationID string) ([]decision.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, interpretation_id, action, reason, actor, uncertainties_acknowledged, decided_at
		FROM decisions WHERE interpretation_id = ? ORDER BY id`, interpretationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []decision.Decision{}
	for rows.Next() {
		var d decision.Decision
		var decided string
		if err := rows.Scan(&d.ID, &d.InterpretationID, &d.Action, &d.Reason, &d.Actor,
			&d.UncertaintiesAcknowledged, &decided); err != nil {
			return nil, err
		}
		d.DecidedAt = parseTime(decided)
		out = append(out, d)
	}
	return out, rows.Err()
}
