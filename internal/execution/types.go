// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package execution

import (
	"encoding/json"
	"errors"
	"strings"
)

// CompatibilityState describes whether a provider can satisfy a request.
type CompatibilityState string

const (
	Available   CompatibilityState = "AVAILABLE"
	Degraded    CompatibilityState = "DEGRADED"
	Unavailable CompatibilityState = "UNAVAILABLE"
)

// State describes what happened during this execution attempt.
type State string

const (
	Collected State = "COLLECTED"
	Partial   State = "PARTIAL"
	Skipped   State = "SKIPPED"
	Failed    State = "FAILED"
	Blocked   State = "BLOCKED"
)

// Reason is a stable machine-readable explanation.
type Reason string

const (
	ReasonNone                  Reason = "NONE"
	ReasonPrivilegeRequired     Reason = "PRIVILEGE_REQUIRED"
	ReasonAPIUnavailable        Reason = "API_UNAVAILABLE"
	ReasonDependencyMissing     Reason = "DEPENDENCY_MISSING"
	ReasonTargetStateRestricted Reason = "TARGET_STATE_RESTRICTED"
	ReasonTimeout               Reason = "TIMEOUT"
	ReasonProviderError         Reason = "PROVIDER_ERROR"
	ReasonPolicyDisabled        Reason = "POLICY_DISABLED"
	ReasonUnsupportedOS         Reason = "UNSUPPORTED_OS"
	ReasonUnsupportedArch       Reason = "UNSUPPORTED_ARCH"
)

// Result is returned by a provider. Compatibility is intentionally absent.
type Result struct {
	State             State           `json:"state"`
	Reason            Reason          `json:"reason"`
	Detail            string          `json:"detail,omitempty"`
	SideEffectSummary string          `json:"side_effect_summary"`
	Payload           json.RawMessage `json:"payload,omitempty"`
}

// Validate checks the existing execution contract and the synthetic Payload
// boundary. Payload is retained only for synthetic compatibility; it is not a
// general artifact transport contract.
func (r Result) Validate() error {
	if !r.State.Valid() {
		return errors.New("execution result state is invalid")
	}
	if !r.Reason.Valid() {
		return errors.New("execution result reason is invalid")
	}
	if strings.TrimSpace(r.SideEffectSummary) == "" {
		return errors.New("execution result side effect summary is required")
	}
	if len(r.Payload) == 0 {
		return nil
	}
	if r.State != Collected && r.State != Partial {
		return errors.New("synthetic payload requires COLLECTED or PARTIAL execution")
	}
	if !json.Valid(r.Payload) {
		return errors.New("synthetic payload must contain valid JSON")
	}
	return nil
}

// Valid reports whether a compatibility state is defined by v0.
func (s CompatibilityState) Valid() bool {
	return s == Available || s == Degraded || s == Unavailable
}

// Valid reports whether an execution state is defined by v0.
func (s State) Valid() bool {
	switch s {
	case Collected, Partial, Skipped, Failed, Blocked:
		return true
	default:
		return false
	}
}

// Successful is deliberately narrow: partial and blocked are not complete.
func (s State) Successful() bool {
	return s == Collected
}

// Valid reports whether a reason is defined by v0.
func (r Reason) Valid() bool {
	switch r {
	case ReasonNone, ReasonPrivilegeRequired, ReasonAPIUnavailable,
		ReasonDependencyMissing, ReasonTargetStateRestricted, ReasonTimeout,
		ReasonProviderError, ReasonPolicyDisabled, ReasonUnsupportedOS,
		ReasonUnsupportedArch:
		return true
	default:
		return false
	}
}
