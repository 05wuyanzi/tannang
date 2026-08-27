// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

// WindowsHostOSIdentitySnapshotID identifies the bounded host/OS identity
// evidence capability. It deliberately describes the requested fact rather
// than the Windows APIs used by its Provider.
const WindowsHostOSIdentitySnapshotID = "WINDOWS_HOST_OS_IDENTITY_SNAPSHOT"

// WindowsHostOSIdentitySnapshot returns the fixed supplemental v0 capability.
func WindowsHostOSIdentitySnapshot() Capability {
	return Capability{
		ID:                   WindowsHostOSIdentitySnapshotID,
		Description:          "Capture the local Windows host name, OS version/build, and native architecture.",
		AcquisitionSemantics: StateSnapshot,
		Sensitivity:          "medium",
	}
}
