// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !windows

package fingerprint

import (
	"context"
	"runtime"
	"time"
)

func platformProbe(context.Context, string, Options) (TargetFingerprint, error) {
	now := time.Now().UTC()
	unsupportedString := func() Field[string] {
		return unsupportedField[string]("platform", "NON_WINDOWS_UNSUPPORTED", now)
	}
	unsupportedUint32 := func() Field[uint32] {
		return unsupportedField[uint32]("platform", "NON_WINDOWS_UNSUPPORTED", now)
	}
	unsupportedUint64 := func() Field[uint64] {
		return unsupportedField[uint64]("platform", "NON_WINDOWS_UNSUPPORTED", now)
	}
	unsupportedBool := func() Field[bool] {
		return unsupportedField[bool]("platform", "NON_WINDOWS_UNSUPPORTED", now)
	}
	return TargetFingerprint{
		Platform: runtime.GOOS,
		Probe: &ProbeFields{
			OSVersion:                    unsupportedString(),
			OSBuild:                      unsupportedString(),
			NativeArchitecture:           unsupportedString(),
			ProcessArchitecture:          knownField(runtime.GOARCH, "runtime.GOARCH", now),
			LogicalProcessorCount:        unsupportedUint32(),
			TotalPhysicalMemoryBytes:     unsupportedUint64(),
			AvailablePhysicalMemoryBytes: unsupportedUint64(),
			Elevated:                     unsupportedBool(),
			TokenElevationType:           unsupportedString(),
			OutputVolume: OutputVolumeFacts{
				ValidatedOutputPath:  unsupportedString(),
				VolumeRoot:           unsupportedString(),
				DriveType:            unsupportedString(),
				FileSystem:           unsupportedString(),
				AvailableBytesCaller: unsupportedUint64(),
			},
		},
	}, ErrUnsupportedPlatform
}
