package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations are applied in order; PRAGMA user_version records how many
// have run. Append only.
var migrations = []string{
	`
CREATE TABLE components (
	id      INTEGER PRIMARY KEY,
	purl    TEXT NOT NULL UNIQUE,
	package TEXT NOT NULL,
	version TEXT NOT NULL
);
CREATE INDEX components_package ON components(package);

CREATE TABLE license_texts (
	hash            TEXT PRIMARY KEY,
	normalized_hash TEXT NOT NULL,
	text            TEXT NOT NULL
);

CREATE TABLE evidence (
	id              TEXT PRIMARY KEY,
	component_id    INTEGER NOT NULL REFERENCES components(id),
	spdx_expression TEXT NOT NULL,
	text_hash       TEXT NOT NULL REFERENCES license_texts(hash),
	source          TEXT NOT NULL,
	collected_at    TEXT NOT NULL,
	UNIQUE (component_id, spdx_expression, text_hash)
);
CREATE INDEX evidence_text_hash ON evidence(text_hash);

CREATE TABLE interpretations (
	id           TEXT PRIMARY KEY,
	text_hash    TEXT NOT NULL REFERENCES license_texts(hash),
	license      TEXT NOT NULL,
	provider     TEXT NOT NULL,
	model        TEXT NOT NULL,
	skill        TEXT NOT NULL,
	skill_version TEXT NOT NULL,
	body         TEXT NOT NULL,
	status       TEXT NOT NULL,
	review_note  TEXT NOT NULL DEFAULT '',
	submitted_at TEXT NOT NULL
);
CREATE INDEX interpretations_text_hash ON interpretations(text_hash, status);

CREATE TABLE decisions (
	id                         INTEGER PRIMARY KEY,
	interpretation_id          TEXT NOT NULL REFERENCES interpretations(id),
	action                     TEXT NOT NULL,
	reason                     TEXT NOT NULL,
	actor                      TEXT NOT NULL,
	uncertainties_acknowledged INTEGER NOT NULL,
	decided_at                 TEXT NOT NULL
);
CREATE INDEX decisions_interpretation ON decisions(interpretation_id);
`,
	`
CREATE TABLE ingest_runs (
	id           TEXT PRIMARY KEY,
	source       TEXT NOT NULL,
	run_dir      TEXT NOT NULL,
	artifact_ref TEXT NOT NULL,
	code_commit  TEXT NOT NULL,
	rule_version TEXT NOT NULL,
	input_sha256 TEXT NOT NULL UNIQUE,
	ingested_at  TEXT NOT NULL
);

-- One row per upstream scope record. Unresolved and skipped records are
-- kept with their reason so nothing silently drops out of review.
CREATE TABLE scope_records (
	run_id                 TEXT NOT NULL REFERENCES ingest_runs(id),
	record_id              TEXT NOT NULL,
	relation               TEXT NOT NULL,
	subject                TEXT NOT NULL,
	owner_ref              TEXT NOT NULL,
	component_id           INTEGER REFERENCES components(id),
	declared_expression    TEXT NOT NULL,
	inclusion              TEXT NOT NULL,
	observed_applicability TEXT NOT NULL,
	evidence_id            TEXT REFERENCES evidence(id),
	text_origin            TEXT NOT NULL,
	holders                TEXT NOT NULL,
	outcome                TEXT NOT NULL,
	reason                 TEXT NOT NULL,
	detail                 TEXT NOT NULL,
	PRIMARY KEY (run_id, record_id)
);
CREATE INDEX scope_records_evidence ON scope_records(evidence_id);
`,
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
