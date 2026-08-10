// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

// ProcessIdentitySnapshotID identifies the minimal Windows process identity
// snapshot capability.
const ProcessIdentitySnapshotID = "PROCESS_IDENTITY_SNAPSHOT"

// ProcessIdentitySnapshot returns the fixed v0 capability definition.
func ProcessIdentitySnapshot() Capability {
	return Capability{
		ID:                   ProcessIdentitySnapshotID,
		Description:          "Capture a minimal snapshot of visible Windows process identities: process ID, parent process ID, and executable name.",
		AcquisitionSemantics: StateSnapshot,
		Sensitivity:          "medium",
	}
}
