package hoplites

import (
	"path/filepath"

	"github.com/sensibile/thesmothetai/internal/store"
)

// Source names Hoplites as the origin of ingested evidence.
const Source = "hoplites"

// IngestInput resolves every record of run against files.
func IngestInput(run *Run, files Files) store.IngestInput {
	dir, err := filepath.Abs(run.Dir)
	if err != nil {
		dir = run.Dir
	}
	in := store.IngestInput{
		Source:      Source,
		RunDir:      dir,
		ArtifactRef: run.ArtifactRef,
		CodeCommit:  run.CodeCommit,
		RuleVersion: run.RuleVersion,
		InputSHA256: run.ScopesSHA256,
		Records:     make([]store.ScopeInput, 0, len(run.Records)),
	}
	for _, rec := range run.Records {
		res := Resolve(rec, files)
		purl, _ := run.PURL(rec)
		si := store.ScopeInput{
			RecordID:              rec.ID,
			Relation:              rec.Relation,
			Subject:               rec.Subject,
			OwnerRef:              rec.OwnerBOMRef,
			PURL:                  purl,
			DeclaredExpression:    res.SPDX,
			Inclusion:             rec.Inclusion,
			ObservedApplicability: rec.Applicability,
			Outcome:               string(res.Outcome),
			Reason:                res.Reason,
			Detail:                res.Detail,
			TextOrigin:            res.Origin,
			Holders:               res.Holders,
		}
		if res.Outcome == Linked {
			si.Text = res.Text
		}
		in.Records = append(in.Records, si)
	}
	return in
}
