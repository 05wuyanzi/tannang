// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

// WindowsTransportEndpointSnapshotID identifies the bounded local Windows
// TCP/UDP transport endpoint snapshot capability.
const WindowsTransportEndpointSnapshotID = "WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT"

// WindowsTransportEndpointSnapshot returns the fixed supplemental v0
// capability definition. It describes the requested local evidence, not the
// native API used by its Provider.
func WindowsTransportEndpointSnapshot() Capability {
	return Capability{
		ID:                   WindowsTransportEndpointSnapshotID,
		Description:          "Capture a bounded snapshot of local Windows TCP and UDP transport endpoints with owning process IDs.",
		AcquisitionSemantics: StateSnapshot,
		Sensitivity:          "medium",
	}
}
