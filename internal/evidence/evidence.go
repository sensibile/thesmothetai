// Package evidence defines components and the license evidence collected
// for them.
package evidence

import (
	"fmt"
	"strings"
	"time"
)

// Component is a package at a specific version, identified by its purl.
type Component struct {
	ID      int64  `json:"-"`
	PURL    string `json:"purl"`
	Package string `json:"package"`
	Version string `json:"version"`
}

// Evidence is one observation of a license for a component.
type Evidence struct {
	ID             string    `json:"id"`
	Component      Component `json:"component"`
	SPDXExpression string    `json:"spdx_expression"`
	TextHash       string    `json:"text_hash"`
	NormalizedHash string    `json:"normalized_hash"`
	Source         string    `json:"source"`
	CollectedAt    time.Time `json:"collected_at"`
}

// ParsePURL splits a purl into its versionless package key and version.
// Qualifiers and subpath are dropped from the package key.
func ParsePURL(purl string) (Component, error) {
	if !strings.HasPrefix(purl, "pkg:") {
		return Component{}, fmt.Errorf("purl %q: must start with pkg:", purl)
	}
	base := purl
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	at := strings.LastIndex(base, "@")
	if at < 0 || at < strings.LastIndex(base, "/") {
		return Component{}, fmt.Errorf("purl %q: missing version", purl)
	}
	pkg, version := base[:at], base[at+1:]
	if version == "" || !strings.Contains(pkg, "/") {
		return Component{}, fmt.Errorf("purl %q: malformed", purl)
	}
	return Component{PURL: purl, Package: pkg, Version: version}, nil
}
