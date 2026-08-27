// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

import "testing"

func TestCapabilityRequestValidationAndPriorityOrder(t *testing.T) {
	t.Parallel()
	priorities := []RequestPriority{PriorityEarly, PriorityNormal, PriorityLate}
	for index, priority := range priorities {
		request := CapabilityRequest{ID: "SYNTHETIC_BASELINE", Priority: priority}
		if err := request.Validate(); err != nil {
			t.Fatalf("priority %s rejected: %v", priority, err)
		}
		if priority.Order() != index {
			t.Fatalf("priority %s order = %d, want %d", priority, priority.Order(), index)
		}
	}
	for _, request := range []CapabilityRequest{
		{ID: "synthetic", Priority: PriorityNormal},
		{ID: "SYNTHETIC", Priority: "URGENT"},
	} {
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid request unexpectedly validated: %+v", request)
		}
	}
}

func TestValidIDIsSharedCapabilityAuthority(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{
		"SYNTHETIC_BASELINE": true,
		"A1":                 true,
		"":                   false,
		"lowercase":          false,
		"WITH-DASH":          false,
		"WITH SPACE":         false,
	} {
		if got := ValidID(value); got != want {
			t.Fatalf("ValidID(%q) = %t, want %t", value, got, want)
		}
	}
}
