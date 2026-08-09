// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package fingerprint

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFieldValueStateInvariant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	known := knownField(uint64(0), "synthetic", now)
	if err := known.Validate("known"); err != nil {
		t.Fatalf("KNOWN zero value should be valid when explicitly measured: %v", err)
	}

	missingValue := Field[uint64]{State: Known, Source: "synthetic", CapturedAt: now.Format(time.RFC3339Nano)}
	if err := missingValue.Validate("missing"); err == nil {
		t.Fatal("KNOWN field without value unexpectedly validated")
	}

	fabricated := failedField[uint64]("synthetic", &apiError{reason: "API_CALL_FAILED"}, now)
	value := uint64(1)
	fabricated.Value = &value
	if err := fabricated.Validate("fabricated"); err == nil {
		t.Fatal("FAILED field with a value unexpectedly validated")
	}
}

func TestKnownStringFieldRequiresMeaningfulValue(t *testing.T) {
	t.Parallel()
	now := testTime()
	for name, value := range map[string]string{
		"empty":      "",
		"whitespace": " \t\r\n",
		"non-empty":  "NTFS",
	} {
		field := knownField(value, "synthetic", now)
		err := field.Validate(name)
		if name == "non-empty" && err != nil {
			t.Fatalf("valid KNOWN string rejected: %v", err)
		}
		if name != "non-empty" && err == nil {
			t.Fatalf("%s KNOWN string unexpectedly validated", name)
		}
	}
}

func TestSyntheticFingerprintStillRequiresRuntimeLane(t *testing.T) {
	t.Parallel()
	target := TargetFingerprint{
		Platform:     "windows",
		OSFamily:     "WindowsNT",
		Version:      "synthetic",
		Build:        "0",
		Architecture: "amd64",
		Privilege:    "standard-user",
	}
	if err := target.Validate(); err == nil || !strings.Contains(err.Error(), "runtime_lane") {
		t.Fatalf("Validate() error = %v, want missing runtime_lane", err)
	}
	target.RuntimeLane = "MODERN"
	if err := target.Validate(); err != nil {
		t.Fatalf("synthetic target no longer validates: %v", err)
	}
}

func TestRealFingerprintAllowsNoRuntimeLaneAndSoftFailures(t *testing.T) {
	t.Parallel()
	target := validRealTarget(t)
	target.Probe.TotalPhysicalMemoryBytes = failedField[uint64]("GlobalMemoryStatusEx", &apiError{reason: "API_CALL_FAILED", code: 5}, testTime())
	target.Probe.AvailablePhysicalMemoryBytes = failedField[uint64]("GlobalMemoryStatusEx", &apiError{reason: "API_CALL_FAILED", code: 5}, testTime())
	if err := target.Validate(); err != nil {
		t.Fatalf("soft memory failure invalidated the whole fingerprint: %v", err)
	}
}

func TestRealFingerprintRejectsUnknownHardField(t *testing.T) {
	t.Parallel()
	target := validRealTarget(t)
	target.Probe.NativeArchitecture = failedField[string]("IsWow64Process2", &apiError{reason: "API_CALL_FAILED"}, testTime())
	if err := target.Validate(); err == nil || !strings.Contains(err.Error(), "hard probe field") {
		t.Fatalf("Validate() error = %v, want hard-field rejection", err)
	}
}

func TestRealFingerprintAllowsSoftTokenFailures(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*TargetFingerprint){
		"elevation failed": func(target *TargetFingerprint) {
			target.Privilege = ""
			target.Probe.Elevated = failedField[bool]("GetTokenInformation", &apiError{reason: "TOKEN_ELEVATION_QUERY_FAILED", code: 5}, testTime())
			target.Probe.TokenElevationType = failedField[string]("GetTokenInformation", &apiError{reason: "TOKEN_ELEVATION_QUERY_FAILED", code: 5}, testTime())
		},
		"token type unsupported": func(target *TargetFingerprint) {
			target.Privilege = ""
			target.Probe.TokenElevationType = unsupportedField[string]("GetTokenInformation", "API_UNAVAILABLE", testTime())
		},
	} {
		t.Run(name, func(t *testing.T) {
			target := validRealTarget(t)
			mutate(&target)
			if err := target.Validate(); err != nil {
				t.Fatalf("soft token failure invalidated base fingerprint: %v", err)
			}
		})
	}
}

func TestRealFingerprintRejectsMalformedOrContradictoryTokenContext(t *testing.T) {
	t.Parallel()
	t.Run("malformed known token type", func(t *testing.T) {
		target := validRealTarget(t)
		target.Probe.TokenElevationType = knownField("unexpected", "GetTokenInformation(TokenElevationType)", testTime())
		if err := target.Validate(); err == nil {
			t.Fatal("malformed KNOWN token type unexpectedly validated")
		}
	})
	t.Run("contradictory privilege mirror", func(t *testing.T) {
		target := validRealTarget(t)
		target.Privilege = "elevated"
		if err := target.Validate(); err == nil {
			t.Fatal("contradictory privilege mirror unexpectedly validated")
		}
	})
}

func TestRealFingerprintRejectsEmptyKnownStringProbeFields(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*TargetFingerprint){
		"output path": func(target *TargetFingerprint) {
			target.Probe.OutputVolume.ValidatedOutputPath = knownField("", "pathsafe.ValidateOutputPath", testTime())
		},
		"volume root": func(target *TargetFingerprint) {
			target.Probe.OutputVolume.VolumeRoot = knownField("", "filepath.VolumeName", testTime())
		},
		"token type": func(target *TargetFingerprint) {
			target.Probe.TokenElevationType = knownField("", "GetTokenInformation(TokenElevationType)", testTime())
		},
	} {
		t.Run(name, func(t *testing.T) {
			target := validRealTarget(t)
			mutate(&target)
			if err := target.Validate(); err == nil {
				t.Fatalf("empty KNOWN %s unexpectedly validated", name)
			}
		})
	}
}

func TestFailedFieldJSONOmitsValue(t *testing.T) {
	t.Parallel()
	field := failedField[uint64]("GlobalMemoryStatusEx", &apiError{reason: "API_CALL_FAILED", code: 5}, testTime())
	data, err := json.Marshal(field)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"value"`) {
		t.Fatalf("failed field JSON contains a fabricated value: %s", data)
	}
	if !strings.Contains(string(data), `"error_code":5`) {
		t.Fatalf("failed field JSON lost the Win32 error code: %s", data)
	}
}

func TestPartialRealFingerprintJSONOmitsUnknownCompatibilityMirrors(t *testing.T) {
	t.Parallel()
	target := validRealTarget(t)
	target.Version = ""
	target.Build = ""
	target.Privilege = ""
	target.Probe.OSVersion = unsupportedField[string]("RtlGetVersion", "API_UNAVAILABLE", testTime())
	target.Probe.OSBuild = unsupportedField[string]("RtlGetVersion", "API_UNAVAILABLE", testTime())
	target.Probe.Elevated = failedField[bool]("GetTokenInformation", &apiError{reason: "API_CALL_FAILED"}, testTime())
	target.Probe.TokenElevationType = failedField[string]("GetTokenInformation", &apiError{reason: "API_CALL_FAILED"}, testTime())

	data, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"version", "build", "privilege", "elevated"} {
		if _, exists := topLevel[forbidden]; exists {
			t.Fatalf("partial fingerprint JSON contains unresolved top-level mirror %s: %s", forbidden, data)
		}
	}
	if !strings.Contains(string(data), `"probe"`) || !strings.Contains(string(data), `"state":"UNSUPPORTED"`) {
		t.Fatalf("partial fingerprint JSON lost field-level failure state: %s", data)
	}
	if err := target.Validate(); err == nil {
		t.Fatal("partial fingerprint unexpectedly became resolver-ready")
	}
}

func validRealTarget(t *testing.T) TargetFingerprint {
	t.Helper()
	now := testTime()
	target := TargetFingerprint{
		Platform:     "windows",
		OSFamily:     "WindowsNT",
		Version:      "10.0",
		Build:        "26100",
		Architecture: "amd64",
		Privilege:    "standard-user",
		Elevated:     false,
		Probe: &ProbeFields{
			OSVersion:                    knownField("10.0", "RtlGetVersion", now),
			OSBuild:                      knownField("26100", "RtlGetVersion", now),
			NativeArchitecture:           knownField("amd64", "IsWow64Process2", now),
			ProcessArchitecture:          knownField("amd64", "runtime.GOARCH", now),
			LogicalProcessorCount:        knownField(uint32(8), "GetActiveProcessorCount", now),
			TotalPhysicalMemoryBytes:     knownField(uint64(16<<30), "GlobalMemoryStatusEx", now),
			AvailablePhysicalMemoryBytes: knownField(uint64(8<<30), "GlobalMemoryStatusEx", now),
			Elevated:                     knownField(false, "GetTokenInformation(TokenElevation)", now),
			TokenElevationType:           knownField("default", "GetTokenInformation(TokenElevationType)", now),
			OutputVolume: OutputVolumeFacts{
				ValidatedOutputPath:  knownField(`C:\safe\output`, "pathsafe.ValidateOutputPath", now),
				VolumeRoot:           knownField(`C:\`, "filepath.VolumeName", now),
				DriveType:            knownField("FIXED", "GetDriveTypeW", now),
				FileSystem:           knownField("NTFS", "GetVolumeInformationW", now),
				AvailableBytesCaller: knownField(uint64(1<<30), "GetDiskFreeSpaceExW", now),
			},
		},
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("test target is invalid: %v", err)
	}
	return target
}

func testTime() time.Time {
	return time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
}
