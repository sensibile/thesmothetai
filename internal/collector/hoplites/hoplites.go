// Package hoplites reads the license scope records a Hoplites image run
// produces and resolves each one to the verbatim text Thesmothetai
// interprets.
//
// Hoplites owns discovery and inclusion: which notices exist in an
// artifact and whether their code is shipped. Thesmothetai takes it from
// there. Hoplites' own applicability and fulfillment values are kept only
// as observations.
package hoplites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RuleVersion is the license-scopes.json version this package understands.
const RuleVersion = "license-scope-v1"

type Record struct {
	ID                 string          `json:"id"`
	Relation           string          `json:"relation"`
	Subject            string          `json:"subject"`
	Component          string          `json:"component"`
	Version            string          `json:"version"`
	OwnerBOMRef        string          `json:"owner_bom_ref"`
	DeclaredExpression *string         `json:"declared_expression"`
	Inclusion          string          `json:"inclusion"`
	Applicability      string          `json:"applicability"`
	Fulfillment        string          `json:"fulfillment"`
	Evidence           json.RawMessage `json:"evidence"`
}

// Run is one Hoplites output directory.
type Run struct {
	Dir         string
	ArtifactRef string // image digest reference and platform
	CodeCommit  string
	RuleVersion string
	// ScopesSHA256 identifies the exact license-scopes.json ingested.
	ScopesSHA256 string
	Records      []Record
	// purls maps normalized BOM bom-refs to purls.
	purls map[string]string
}

// Load reads the run at dir. It rejects unknown rule versions rather than
// guessing at a changed contract.
func Load(dir string) (*Run, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "license-scopes.json"))
	if err != nil {
		return nil, err
	}
	var scopes struct {
		RuleVersion string   `json:"rule_version"`
		Records     []Record `json:"records"`
	}
	if err := json.Unmarshal(raw, &scopes); err != nil {
		return nil, fmt.Errorf("license-scopes.json: %w", err)
	}
	if scopes.RuleVersion != RuleVersion {
		return nil, fmt.Errorf("license-scopes.json: rule_version %q is not supported (want %s)", scopes.RuleVersion, RuleVersion)
	}
	sum := sha256.Sum256(raw)
	run := &Run{
		Dir:          dir,
		RuleVersion:  scopes.RuleVersion,
		ScopesSHA256: "sha256:" + hex.EncodeToString(sum[:]),
		Records:      scopes.Records,
	}

	var image struct {
		ImageRef string `json:"image_ref"`
		Platform string `json:"platform"`
	}
	if err := readJSON(filepath.Join(dir, "image.json"), &image); err != nil {
		return nil, err
	}
	if image.ImageRef == "" {
		return nil, fmt.Errorf("image.json: image_ref is empty")
	}
	run.ArtifactRef = image.ImageRef
	if image.Platform != "" {
		run.ArtifactRef += " " + image.Platform
	}

	var summary struct {
		CodeCommit string `json:"code_commit"`
	}
	if err := readJSON(filepath.Join(dir, "summary.json"), &summary); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	run.CodeCommit = summary.CodeCommit

	var bom struct {
		Components []bomComponent `json:"components"`
	}
	if err := readJSON(filepath.Join(dir, "normalized.cdx.json"), &bom); err != nil {
		return nil, err
	}
	run.purls = map[string]string{}
	indexPURLs(bom.Components, run.purls)
	return run, nil
}

type bomComponent struct {
	BOMRef     string         `json:"bom-ref"`
	PURL       string         `json:"purl"`
	Components []bomComponent `json:"components"`
}

func indexPURLs(cs []bomComponent, out map[string]string) {
	for _, c := range cs {
		if c.BOMRef != "" && c.PURL != "" {
			out[c.BOMRef] = c.PURL
		}
		indexPURLs(c.Components, out)
	}
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// Sources indexes every file under the run's sources directory by its
// SHA-256, so records can be resolved by the hashes Hoplites recorded
// rather than by paths.
func (r *Run) Sources() (Files, error) {
	files := Files{}
	root := filepath.Join(r.Dir, "sources")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(r.Dir, path)
		files[hex.EncodeToString(sum[:])] = File{Path: filepath.ToSlash(rel), Data: data}
		return nil
	})
	return files, err
}

// PURL returns the purl of the record's owner with Syft's run-specific
// package-id qualifier removed, so the same package matches across runs.
func (r *Run) PURL(rec Record) (string, bool) {
	p, ok := r.purls[rec.OwnerBOMRef]
	if !ok && strings.HasPrefix(rec.OwnerBOMRef, "pkg:") {
		p, ok = rec.OwnerBOMRef, true
	}
	if !ok {
		return "", false
	}
	return stripQualifier(p, "package-id"), true
}

func stripQualifier(purl, key string) string {
	base, rest, ok := strings.Cut(purl, "?")
	if !ok {
		return purl
	}
	query, frag, hasFrag := strings.Cut(rest, "#")
	var kept []string
	for _, kv := range strings.Split(query, "&") {
		if k, _, _ := strings.Cut(kv, "="); k != key && kv != "" {
			kept = append(kept, kv)
		}
	}
	out := base
	if len(kept) > 0 {
		out += "?" + strings.Join(kept, "&")
	}
	if hasFrag {
		out += "#" + frag
	}
	return out
}
