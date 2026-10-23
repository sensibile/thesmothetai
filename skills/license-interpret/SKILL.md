---
name: license-interpret
description: Interpret license texts queued in the Thesmothetai registry and submit structured interpretations for human review. Use when asked to work the interpretation queue, interpret a new or changed license text, or run `thesmo interpret`.
---

# License interpretation

You turn a license text into a structured interpretation that a person
will review. You never approve anything. Your output is a proposal.

Skill version: `1` (put this in `provenance.skill_version`).

## Workflow

1. Build the CLI if needed: `go build -o thesmo ./cmd/thesmo`
2. List work, skipping texts you already submitted:
   `./thesmo interpret queue --provider <claude|codex>`
3. For each item, load everything you need:
   `./thesmo interpret context <source_hash>`
   - `text` is the exact license text. Interpret this text, not your memory
     of the SPDX standard text. If it deviates from the standard text,
     say so in `summary` and record the deviation as an uncertainty.
   - If `previous_approved` is present, another version of the package
     shipped a text that a person already approved. Diff the two texts,
     start from the previous obligations, and change only what the changed
     clauses require. Mention the changed clauses in `summary`.
4. Write a submission that matches `./thesmo schema interpretation` and
   submit it: `./thesmo interpret submit -f -` (JSON on stdin).
5. Report the returned `id`, `status` and `review_note`. If the status is
   `review_required`, another provider disagreed; summarize the
   disagreement for the reviewer instead of resubmitting to match it.

## Writing the submission

- `source_hash`: copy exactly from the queue.
- `license`: SPDX identifier, or `LicenseRef-<name>` if the text is not a
  known SPDX license or deviates from it materially.
- `obligations`: one entry per duty the text imposes on someone who uses or
  ships the component.
  - `clause` names the section or paragraph. `quote` is a short verbatim
    excerpt that supports the obligation. Never paraphrase inside `quote`.
  - `triggers` lists conditions that must all hold for the duty to apply.
    Use `[]` only for duties that apply unconditionally.
- `uncertainties`: anything you are not sure about, including ambiguous
  scope, unusual exceptions, and conflicts with how the SPDX expression
  describes the component. Each one forces the reviewer to acknowledge it
  before approval, so be honest but specific.
- `provenance`: your provider (`claude` or `codex`), your exact model ID,
  `skill: "license-interpret"`, and the skill version above.

### Vocabulary

Prefer these identifiers so interpretations from different providers can
be compared mechanically. Add a new snake_case identifier only when none
fits, and mention it in `summary`.

Obligations:

| id | meaning |
|---|---|
| `include_license_text` | ship a copy of the license |
| `preserve_copyright_notice` | keep copyright notices intact |
| `include_notice_file` | ship the NOTICE file contents |
| `state_changes` | mark modified files or describe changes |
| `disclose_source` | make corresponding source available |
| `provide_source_offer` | include a written offer for source |
| `same_license` | license derivatives under the same terms |
| `no_endorsement` | do not use names for endorsement |
| `legal_review` | a lawyer must decide; use when no rule fits |

Triggers:

| id | meaning |
|---|---|
| `distribution` | the component is shipped to third parties in any form |
| `binary_distribution` | shipped in compiled or object form |
| `source_distribution` | shipped in source form |
| `modification` | the component was modified |
| `network_use` | users interact with it over a network |
| `static_linking` | statically linked into another work |

## Rules

- Never run `thesmo review` or otherwise try to record a decision.
- Never resubmit to make your result match another provider's.
- Do not invent clauses. If a duty is implied rather than stated, put it in
  `uncertainties`, not `obligations`.
