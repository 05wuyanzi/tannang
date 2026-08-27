// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

import "testing"

func TestWindowsTransportEndpointSnapshotDefinition(t *testing.T) {
	definition := WindowsTransportEndpointSnapshot()
	if definition.ID != WindowsTransportEndpointSnapshotID {
		t.Fatalf("id=%q", definition.ID)
	}
	if definition.AcquisitionSemantics != StateSnapshot {
		t.Fatalf("acquisition=%q", definition.AcquisitionSemantics)
	}
	if definition.Sensitivity != "medium" {
		t.Fatalf("sensitivity=%q", definition.Sensitivity)
	}
	if err := definition.Validate(); err != nil {
		t.Fatalf("definition invalid: %v", err)
	}
	if definition.Description == "" {
		t.Fatal("description is empty")
	}
}
