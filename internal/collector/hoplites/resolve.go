package hoplites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sensibile/thesmothetai/internal/collector/dep5"
)

// File is one preserved source file of a run.
type File struct {
	Path string
	Data []byte
}

// Files maps hex SHA-256 to file.
type Files map[string]File

type Outcome string

const (
	// Linked records resolved to a license text and become evidence.
	Linked Outcome = "linked"
	// Skipped records carry no license terms by Hoplites' own assessment.
	Skipped Outcome = "skipped"
	// Unresolved records stay on file with the reason; they are never
	// treated as license-free.
	Unresolved Outcome = "unresolved"
)

// Reason codes for skipped and unresolved records. Stable, for counting.
const (
	ReasonNotLicenseTerms    = "not-license-terms"
	ReasonNoTextEvidence     = "no-text-evidence"
	ReasonSourceMissing      = "source-missing"
	ReasonNotMachineReadable = "dep5-not-machine-readable"
	ReasonDEP5LicenseMissing = "dep5-license-missing"
	ReasonRangeInvalid       = "notice-range-invalid"
	ReasonNoticeRangeMissing = "notice-range-not-provided"
	ReasonUnknownRelation    = "unknown-relation"
)

type Resolution struct {
	Outcome Outcome
	Reason  string
	Detail  string
	// Text is the verbatim license text; set only when Linked.
	Text []byte
	// Origin locates Text inside the run directory.
	Origin string
	// Holders are copyright holders kept for notices, never part of Text.
	Holders string
	// SPDX is the expression Hoplites declared for the record.
	SPDX string
}

type evidenceFields struct {
	SHA256              string   `json:"sha256"`
	Copyright           string   `json:"copyright"`
	SourceLabel         string   `json:"source_label"`
	SourceSelector      string   `json:"source_selector"`
	NoticeSHA256        string   `json:"notice_sha256"`
	TextSHA256          string   `json:"text_sha256"`
	ByteStart           *int     `json:"source_byte_start"`
	ByteEnd             *int     `json:"source_byte_end"`
	SourceSHA256        string   `json:"source_sha256"`
	DeclaredExpressions []string `json:"declared_expressions"`
	Expression          string   `json:"expression"`
}

// Resolve finds the license text rec refers to. It is pure: all file
// access goes through files.
func Resolve(rec Record, files Files) Resolution {
	var ev evidenceFields
	if len(rec.Evidence) > 0 {
		if err := json.Unmarshal(rec.Evidence, &ev); err != nil {
			return unresolved(ReasonUnknownRelation, "evidence: "+err.Error())
		}
	}
	res := resolveText(rec.Relation, ev, files)
	res.SPDX = declared(rec, ev)
	return res
}

func resolveText(relation string, ev evidenceFields, files Files) Resolution {
	switch relation {
	case "scanner-evidence-reference":
		return Resolution{Outcome: Skipped, Reason: ReasonNotLicenseTerms}

	case "source-file-declaration":
		f, ok := files[ev.SHA256]
		if !ok {
			return unresolved(ReasonSourceMissing, ev.SHA256)
		}
		// Some files mix legacy prose with DEP-5 paragraphs. Hoplites still
		// read declarations from them, so look the label up anyway and
		// flag the layout in the origin for the reviewer.
		paras := dep5.Parse(f.Data)
		layout := ""
		if !dep5.IsMachineReadable(paras) {
			layout = " [not machine-readable DEP-5]"
		}
		text, missing := dep5.LicenseText(paras, ev.SourceLabel, ev.SourceSelector)
		if len(missing) > 0 {
			reason := ReasonDEP5LicenseMissing
			if layout != "" {
				reason = ReasonNotMachineReadable
			}
			return unresolved(reason, f.Path+": "+strings.Join(missing, ", "))
		}
		return Resolution{
			Outcome: Linked,
			Text:    []byte(text),
			Origin:  fmt.Sprintf("%s (License: %s, Files: %s)%s", f.Path, ev.SourceLabel, ev.SourceSelector, layout),
			Holders: ev.Copyright,
		}

	case "source-document", "assembly-exception-document", "module-legal-document":
		f, ok := files[ev.SHA256]
		if !ok {
			return unresolved(ReasonSourceMissing, ev.SHA256)
		}
		return Resolution{Outcome: Linked, Text: f.Data, Origin: f.Path}
	}

	if ev.ByteStart != nil && ev.ByteEnd != nil && ev.NoticeSHA256 != "" {
		return resolveRange(ev, files)
	}
	switch relation {
	case "installed-source", "compiled-extension", "installed-source-or-generated-module", "binary-embedded-code":
		// The notice lives somewhere inside a source file. A whole source
		// file is not a license text; Hoplites has to locate the notice.
		return unresolved(ReasonNoticeRangeMissing, firstNonEmpty(ev.NoticeSHA256, ev.SourceSHA256))
	case "shipped-runtime-artifact":
		return unresolved(ReasonNoTextEvidence, "")
	}
	return unresolved(ReasonUnknownRelation, relation)
}

func resolveRange(ev evidenceFields, files Files) Resolution {
	f, ok := files[ev.NoticeSHA256]
	if !ok {
		return unresolved(ReasonSourceMissing, ev.NoticeSHA256)
	}
	start, end := *ev.ByteStart, *ev.ByteEnd
	if start < 0 || end > len(f.Data) || start >= end {
		return unresolved(ReasonRangeInvalid, fmt.Sprintf("%s[%d:%d] of %d bytes", f.Path, start, end, len(f.Data)))
	}
	text := f.Data[start:end]
	if ev.TextSHA256 != "" {
		sum := sha256.Sum256(text)
		if hex.EncodeToString(sum[:]) != ev.TextSHA256 {
			return unresolved(ReasonRangeInvalid, fmt.Sprintf("%s[%d:%d] does not match text_sha256", f.Path, start, end))
		}
	}
	return Resolution{Outcome: Linked, Text: text, Origin: fmt.Sprintf("%s[%d:%d]", f.Path, start, end)}
}

func declared(rec Record, ev evidenceFields) string {
	switch {
	case rec.DeclaredExpression != nil && *rec.DeclaredExpression != "":
		return *rec.DeclaredExpression
	case ev.Expression != "":
		return ev.Expression
	case len(ev.DeclaredExpressions) == 1:
		return ev.DeclaredExpressions[0]
	case len(ev.DeclaredExpressions) > 1:
		// Several declarations for one record are alternatives Hoplites
		// observed, not a combined expression.
		return strings.Join(ev.DeclaredExpressions, "; ")
	}
	return "NOASSERTION"
}

func unresolved(reason, detail string) Resolution {
	return Resolution{Outcome: Unresolved, Reason: reason, Detail: detail}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
