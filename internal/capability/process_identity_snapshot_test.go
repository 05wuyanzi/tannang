// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

import "testing"

func TestProcessIdentitySnapshotContract(t *testing.T) {
	t.Parallel()
	request := ProcessIdentitySnapshot()
	if request.ID != ProcessIdentitySnapshotID {
		t.Fatalf("ID = %q, want %q", request.ID, ProcessIdentitySnapshotID)
	}
	if request.Description != "Capture a minimal snapshot of visible Windows process identities: process ID, parent process ID, and executable name." {
		t.Fatalf("unexpected description: %q", request.Description)
	}
	if request.AcquisitionSemantics != StateSnapshot {
		t.Fatalf("acquisition semantics = %q, want STATE_SNAPSHOT", request.AcquisitionSemantics)
	}
	if request.Sensitivity != "medium" {
		t.Fatalf("sensitivity = %q, want medium", request.Sensitivity)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("ProcessIdentitySnapshot().Validate() error: %v", err)
	}
}
