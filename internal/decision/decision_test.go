package decision

import (
	"errors"
	"testing"

	"github.com/sensibile/thesmothetai/internal/interpretation"
)

func TestValidate(t *testing.T) {
	open := interpretation.Interpretation{Status: interpretation.StatusPending}
	uncertain := open
	uncertain.Uncertainties = []interpretation.Uncertainty{{Clause: "4", Reason: "scope unclear"}}
	closed := open
	closed.Status = interpretation.StatusApproved

	ok := Request{Action: ActionApprove, Reason: "matches SPDX MIT", Actor: "me"}

	cases := []struct {
		name string
		it   interpretation.Interpretation
		req  Request
		want error
	}{
		{"ok", open, ok, nil},
		{"blank reason", open, Request{Action: ActionApprove, Reason: "  ", Actor: "me"}, ErrReasonRequired},
		{"no actor", open, Request{Action: ActionReject, Reason: "x"}, ErrActorRequired},
		{"closed", closed, ok, ErrNotOpen},
		{"unacknowledged", uncertain, ok, ErrUncertaintiesNotAcknowledged},
		{"reject ignores ack", uncertain, Request{Action: ActionReject, Reason: "x", Actor: "me"}, nil},
	}
	for _, c := range cases {
		err := Validate(c.it, c.req)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}

	acked := ok
	acked.UncertaintiesAcknowledged = true
	if err := Validate(uncertain, acked); err != nil {
		t.Errorf("acknowledged approval: %v", err)
	}
}
