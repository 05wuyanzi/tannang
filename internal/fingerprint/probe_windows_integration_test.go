// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package fingerprint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsBenignRealProbe(t *testing.T) {
	if os.Getenv("TANNANG_RUN_WINDOWS_FINGERPRINT_INTEGRATION") != "1" {
		t.Skip("set TANNANG_RUN_WINDOWS_FINGERPRINT_INTEGRATION=1 for the bounded real Windows probe")
	}
	output := filepath.Join(t.TempDir(), "future-evidence-package")
	requireFixedVolumeFixture(t, output)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()
	target, err := Probe(ctx, output, Options{IncludeCPUPressure: true})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Probe() error: %v", err)
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("real target Validate() error: %v\ntarget: %+v", err, target)
	}
	if field := target.Probe.OutputVolume.DriveType; field.State != Known || field.Value == nil || *field.Value != "FIXED" {
		t.Fatalf("normal local output volume drive type = %+v, want KNOWN FIXED", field)
	}
	if field := target.Probe.OutputVolume.FileSystem; field.State != Known || field.Value == nil || *field.Value == "" {
		t.Fatalf("normal local output volume filesystem = %+v, want non-empty KNOWN", field)
	}
	if field := target.Probe.OutputVolume.AvailableBytesCaller; field.State != Known || field.Value == nil {
		t.Fatalf("normal local output volume available bytes = %+v, want KNOWN numeric value", field)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("fingerprint probe created the output path: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("probe exceeded bounded integration timeout: %s", elapsed)
	}
	t.Logf(
		"elapsed=%s elevated=%v elevation_type=%v os=%s build=%s native=%s process=%s cpu_state=%s cpu=%v memory_total_state=%s memory_total=%v memory_available_state=%s memory_available=%v drive_state=%s drive=%v filesystem_state=%s filesystem=%v free_state=%s free=%v cpu_pressure_state=%s cpu_pressure=%v",
		elapsed,
		target.Elevated,
		fieldValue(target.Probe.TokenElevationType),
		target.Version,
		target.Build,
		target.Architecture,
		*target.Probe.ProcessArchitecture.Value,
		target.Probe.LogicalProcessorCount.State,
		fieldValue(target.Probe.LogicalProcessorCount),
		target.Probe.TotalPhysicalMemoryBytes.State,
		fieldValue(target.Probe.TotalPhysicalMemoryBytes),
		target.Probe.AvailablePhysicalMemoryBytes.State,
		fieldValue(target.Probe.AvailablePhysicalMemoryBytes),
		target.Probe.OutputVolume.DriveType.State,
		fieldValue(target.Probe.OutputVolume.DriveType),
		target.Probe.OutputVolume.FileSystem.State,
		fieldValue(target.Probe.OutputVolume.FileSystem),
		target.Probe.OutputVolume.AvailableBytesCaller.State,
		fieldValue(target.Probe.OutputVolume.AvailableBytesCaller),
		target.Probe.CPUBusyBasisPoints.State,
		fieldValue(*target.Probe.CPUBusyBasisPoints),
	)
}

func TestFixedVolumeFixtureEligibility(t *testing.T) {
	for name, testCase := range map[string]struct {
		driveType string
		err       error
		eligible  bool
	}{
		"fixed":                   {driveType: "FIXED", eligible: true},
		"removable":               {driveType: "REMOVABLE"},
		"remote":                  {driveType: "REMOTE"},
		"cdrom":                   {driveType: "CDROM"},
		"ramdisk":                 {driveType: "RAMDISK"},
		"unknown drive":           {driveType: "UNKNOWN"},
		"drive query unavailable": {err: errAPIUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			if got := fixedVolumeFixtureEligible(testCase.driveType, testCase.err); got != testCase.eligible {
				t.Fatalf("fixedVolumeFixtureEligible(%q, %v) = %v, want %v", testCase.driveType, testCase.err, got, testCase.eligible)
			}
		})
	}
}

func requireFixedVolumeFixture(t *testing.T, outputPath string) {
	t.Helper()
	volumeRoot := filepath.VolumeName(outputPath) + `\`
	driveType, err := (realWindowsAPI{}).driveType(volumeRoot)
	if !fixedVolumeFixtureEligible(driveType, err) {
		if err != nil {
			t.Skipf("fixed local volume fixture unavailable for %q: %v", volumeRoot, err)
		}
		t.Skipf("fixed local volume fixture unavailable for %q: drive type %s", volumeRoot, driveType)
	}
}

func fixedVolumeFixtureEligible(driveType string, err error) bool {
	return err == nil && driveType == "FIXED"
}

func fieldValue[T any](field Field[T]) any {
	if field.Value == nil {
		return nil
	}
	return *field.Value
}
