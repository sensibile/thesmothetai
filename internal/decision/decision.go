// Package decision holds the rules for a person's verdict on an
// interpretation. The rules live here, not in any UI, so every entry point
// enforces the same constraints.
package decision

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sensibile/thesmothetai/internal/interpretation"
)

type Action string

const (
	ActionApprove        Action = "approve"
	ActionReject         Action = "reject"
	ActionRequestChanges Action = "request_changes"
)

// ResultStatus is the interpretation status an action leads to.
func (a Action) ResultStatus() (interpretation.Status, error) {
	switch a {
	case ActionApprove:
		return interpretation.StatusApproved, nil
	case ActionReject:
		return interpretation.StatusRejected, nil
	case ActionRequestChanges:
		return interpretation.StatusChangesRequested, nil
	}
	return "", fmt.Errorf("unknown action %q", a)
}

type Request struct {
	InterpretationID string `json:"interpretation_id"`
	Action           Action `json:"action"`
	Reason           string `json:"reason"`
	Actor            string `json:"actor"`
	// UncertaintiesAcknowledged records that the reviewer read every
	// uncertainty the interpreter reported.
	UncertaintiesAcknowledged bool `json:"uncertainties_acknowledged"`
}

type Decision struct {
	ID int64 `json:"id"`
	Request
	DecidedAt time.Time `json:"decided_at"`
}

var (
	ErrReasonRequired               = errors.New("a reason is required")
	ErrActorRequired                = errors.New("an actor is required")
	ErrNotOpen                      = errors.New("interpretation is not awaiting a decision")
	ErrUncertaintiesNotAcknowledged = errors.New("uncertainties must be acknowledged before approval")
)

// Validate checks req against the current state of it.
func Validate(it interpretation.Interpretation, req Request) error {
	if _, err := req.Action.ResultStatus(); err != nil {
		return err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return ErrReasonRequired
	}
	if strings.TrimSpace(req.Actor) == "" {
		return ErrActorRequired
	}
	if !it.Status.Open() {
		return fmt.Errorf("%w (status %s)", ErrNotOpen, it.Status)
	}
	if req.Action == ActionApprove && len(it.Uncertainties) > 0 && !req.UncertaintiesAcknowledged {
		return ErrUncertaintiesNotAcknowledged
	}
	return nil
}
