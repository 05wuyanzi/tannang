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
