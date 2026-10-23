# Thesmothetai

License compliance through reusable decisions. Go CLI (`thesmo`) with a
deterministic core; Claude Code and Codex act as interpreters through the
skills in `skills/`.

Read `.taeguk/rules/index.md` before planning and apply Taeguk rules to explicitly assigned workflows.

## Commands

```bash
go build -o thesmo ./cmd/thesmo
go test ./...
go vet ./...
```

The database defaults to `.thesmothetai/thesmo.db`; override with `--db` or
`THESMO_DB`.

## Layout

- `cmd/thesmo` — CLI. Every command prints JSON except `review`.
- `internal/collector/hoplites` — reads Hoplites image runs
  (`license-scopes.json` + `sources/`) and resolves each record to a text.
- `internal/collector/dep5` — Debian machine-readable copyright parsing.
- `internal/fingerprint` — exact and normalized text hashes.
- `internal/evidence` — components (purl) and license evidence.
- `internal/interpretation` — submission contract, schema validation,
  cross-provider comparison.
- `internal/decision` — rules for a person's verdict.
- `internal/store` — SQLite registry, migrations, reuse and change detection.
- `internal/review` — terminal UI for decisions.
- `schemas/` — JSON contracts shared with agents.
- `skills/` — agent skills.

## Hoplites boundary

Hoplites owns discovery and inclusion; Thesmothetai owns interpretation,
applicability, obligations and fulfillment. See `docs/hoplites-boundary.md`.
Do not add a Dependency-Track integration here.

## Trust boundaries

- Interpretations are keyed by the exact text hash. An approved
  interpretation is reused for every component shipping that text.
- Agents submit with `thesmo interpret submit`; the CLI validates against
  `schemas/interpretation.v1.json`. Submissions are never approved
  automatically.
- Only a person records decisions, through `thesmo review`, which refuses to
  run without a terminal. Do not add a non-interactive approval path, and do
  not run `thesmo review` as an agent.
- Decision rules belong in `internal/decision`, not in the UI.

## Interpreting licenses

Follow `skills/license-interpret/SKILL.md`.
