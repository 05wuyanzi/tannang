// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package execution

import (
	"encoding/json"
	"testing"
)

func TestResultValidationPreservesSyntheticPayloadBoundary(t *testing.T) {
	t.Parallel()
	for _, result := range []Result{
		{State: Collected, Reason: ReasonNone, SideEffectSummary: "Synthetic fixture only."},
		{State: Partial, Reason: ReasonAPIUnavailable, SideEffectSummary: "Synthetic fixture only."},
		{State: Collected, Reason: ReasonNone, SideEffectSummary: "Synthetic fixture only.", Payload: json.RawMessage(`{"ok":true}`)},
	} {
		if err := result.Validate(); err != nil {
			t.Fatalf("valid result rejected: %+v: %v", result, err)
		}
	}
}

func TestResultValidationRejectsInvalidSyntheticPayloadOrSummary(t *testing.T) {
	t.Parallel()
	for name, result := range map[string]Result{
		"blank summary": {State: Collected, Reason: ReasonNone},
		"invalid json": {
			State: Collected, Reason: ReasonNone, SideEffectSummary: "Synthetic fixture only.", Payload: json.RawMessage(`{`),
		},
		"payload on failed": {
			State: Failed, Reason: ReasonProviderError, SideEffectSummary: "Synthetic fixture only.", Payload: json.RawMessage(`{"unexpected":true}`),
		},
		"invalid state":  {State: "UNKNOWN", Reason: ReasonNone, SideEffectSummary: "Synthetic fixture only."},
		"invalid reason": {State: Failed, Reason: "UNKNOWN", SideEffectSummary: "Synthetic fixture only."},
	} {
		if err := result.Validate(); err == nil {
			t.Fatalf("%s unexpectedly validated", name)
		}
	}
}

func TestReasonCancelledIsTheOnlyExecutionReasonAddition(t *testing.T) {
	t.Parallel()
	reasons := []Reason{
		ReasonNone,
		ReasonPrivilegeRequired,
		ReasonAPIUnavailable,
		ReasonDependencyMissing,
		ReasonTargetStateRestricted,
		ReasonTimeout,
		ReasonCancelled,
		ReasonProviderError,
		ReasonPolicyDisabled,
		ReasonUnsupportedOS,
		ReasonUnsupportedArch,
	}
	if len(reasons) != 11 {
		t.Fatalf("reason count = %d, want 11", len(reasons))
	}
	seen := make(map[Reason]struct{}, len(reasons))
	for _, reason := range reasons {
		if !reason.Valid() {
			t.Fatalf("reason %q is not valid", reason)
		}
		if _, duplicate := seen[reason]; duplicate {
			t.Fatalf("duplicate reason %q", reason)
		}
		seen[reason] = struct{}{}
	}
	if Reason("UNKNOWN").Valid() {
		t.Fatal("unknown execution reason unexpectedly validated")
	}
}

func TestCancelledExecutionResultsValidate(t *testing.T) {
	t.Parallel()
	for _, state := range []State{Failed, Partial} {
		result := Result{
			State:             state,
			Reason:            ReasonCancelled,
			SideEffectSummary: "Provider execution was explicitly cancelled.",
		}
		if err := result.Validate(); err != nil {
			t.Fatalf("%s/CANCELLED result rejected: %v", state, err)
		}
	}
}
