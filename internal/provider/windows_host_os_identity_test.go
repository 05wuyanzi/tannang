// This Source Code Form is subject to the terms of the Mozilla Public
// License, version 2.0. If a copy of the MPL was not distributed with this
// file, you may obtain a copy at http://mozilla.org/MPL/2.0/.

package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"unicode/utf16"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

type fakeHostOSIdentityAPI struct {
	name                string
	major, minor, build uint32
	arch                string
	available           execution.Reason
	err                 error
	nameErr             error
	versionErr          error
	archErr             error
	cancelOnVersion     context.CancelFunc
	nameCalls           int
}

func (f fakeHostOSIdentityAPI) availability() (execution.Reason, error) { return f.available, f.err }
func (f fakeHostOSIdentityAPI) computerNameEx(buffer []uint16, size *uint32) (bool, error) {
	// The value receiver is intentionally sufficient for ordinary fake behavior.
	if f.nameErr != nil {
		return false, f.nameErr
	}
	if len(buffer) == 0 {
		*size = uint32(len([]rune(f.name)) + 1)
		return false, errors.New("ERROR_MORE_DATA")
	}
	name := utf16.Encode([]rune(f.name))
	if uint32(len(name)+1) > *size {
		*size = uint32(len(name) + 1)
		return false, errors.New("ERROR_MORE_DATA")
	}
	copy(buffer, name)
	*size = uint32(len(name))
	return true, nil
}
func (f fakeHostOSIdentityAPI) osVersion() (uint32, uint32, uint32, error) {
	if f.cancelOnVersion != nil {
		f.cancelOnVersion()
	}
	return f.major, f.minor, f.build, f.versionErr
}
func (f fakeHostOSIdentityAPI) nativeArchitecture() (string, error) { return f.arch, f.archErr }

type failingHostWriter struct {
	err   error
	short bool
}

func (w failingHostWriter) Write(p []byte) (int, error) {
	if w.short {
		return 0, nil
	}
	return 0, w.err
}

type cancellingHostWriter struct{ cancel context.CancelFunc }

func (w cancellingHostWriter) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

func hostIdentityTestTarget() fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "10.0", Build: "26200", Architecture: "amd64", Privilege: "standard-user", Elevated: false, RuntimeLane: "portable"}
}

func TestWindowsHostOSIdentityRunnerCollectsBoundedJSON(t *testing.T) {
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{name: "HOST-01", major: 10, minor: 0, build: 26200, arch: "amd64", available: execution.ReasonNone})
	var out bytes.Buffer
	result := runner.ExecuteTo(context.Background(), capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), &out)
	if result.State != execution.Collected || result.Reason != execution.ReasonNone {
		t.Fatalf("result=%+v", result)
	}
	if got := out.String(); got != `{"computer_name":"HOST-01","os_major":10,"os_minor":0,"os_build":26200,"native_architecture":"amd64"}` {
		t.Fatalf("artifact=%q", got)
	}
}

func TestWindowsHostOSIdentityRunnerRejectsUnsupportedArchitecture(t *testing.T) {
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{name: "HOST", major: 10, minor: 0, build: 1, arch: "arm64", available: execution.ReasonNone})
	result := runner.ExecuteTo(context.Background(), capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), &bytes.Buffer{})
	if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("result=%+v", result)
	}
}

func TestWindowsHostOSIdentityRunnerDoesNotWriteAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{name: "HOST", major: 10, minor: 0, build: 1, arch: "amd64", available: execution.ReasonNone})
	var out bytes.Buffer
	result := runner.ExecuteTo(ctx, capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), &out)
	if result.State != execution.Failed || result.Reason != execution.ReasonCancelled || out.Len() != 0 {
		t.Fatalf("result=%+v bytes=%d", result, out.Len())
	}
}

func TestWindowsHostOSIdentityRunnerFailsClosedForNativeFailures(t *testing.T) {
	for name, api := range map[string]fakeHostOSIdentityAPI{
		"hostname":     {name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone, nameErr: errors.New("name failed")},
		"version":      {name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone, versionErr: errors.New("version failed")},
		"architecture": {name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone, archErr: errors.New("arch failed")},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			result := newWindowsHostOSIdentityRunner(api).ExecuteTo(context.Background(), capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), &out)
			if result.State != execution.Failed || result.Reason != execution.ReasonProviderError || out.Len() != 0 {
				t.Fatalf("result=%+v bytes=%d", result, out.Len())
			}
		})
	}
}

func TestWindowsHostOSIdentityRunnerRejectsWriterFailureAndShortWrite(t *testing.T) {
	api := fakeHostOSIdentityAPI{name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone}
	for name, writer := range map[string]io.Writer{
		"error":       failingHostWriter{err: errors.New("write failed")},
		"no progress": failingHostWriter{short: true},
	} {
		t.Run(name, func(t *testing.T) {
			result := newWindowsHostOSIdentityRunner(api).ExecuteTo(context.Background(), capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), writer)
			if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestWindowsHostOSIdentityRunnerDiscardsAfterWriteCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone})
	var out bytes.Buffer
	writer := cancellingHostWriter{cancel: cancel}
	result := runner.ExecuteTo(ctx, capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), writer)
	if result.State != execution.Failed || result.Reason != execution.ReasonCancelled {
		t.Fatalf("result=%+v", result)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output in unrelated buffer: %d", out.Len())
	}
}

func TestWindowsHostOSIdentityRunnerCancelsAfterObservationBeforeWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	api := fakeHostOSIdentityAPI{name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone, cancelOnVersion: cancel}
	runner := newWindowsHostOSIdentityRunner(api)
	var out bytes.Buffer
	result := runner.ExecuteTo(ctx, capability.WindowsHostOSIdentitySnapshot(), hostIdentityTestTarget(), &out)
	if result.State != execution.Failed || result.Reason != execution.ReasonCancelled || out.Len() != 0 {
		t.Fatalf("result=%+v bytes=%d", result, out.Len())
	}
}

func TestWindowsHostOSIdentityRunnerRejectsWrongRequestAndTarget(t *testing.T) {
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{name: "HOST", major: 10, arch: "amd64", available: execution.ReasonNone})
	wrongCapability := capability.ProcessIdentitySnapshot()
	if result := runner.ExecuteTo(context.Background(), wrongCapability, hostIdentityTestTarget(), &bytes.Buffer{}); result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("wrong capability result=%+v", result)
	}
	wrongTarget := hostIdentityTestTarget()
	wrongTarget.Platform = "linux"
	if result := runner.ExecuteTo(context.Background(), capability.WindowsHostOSIdentitySnapshot(), wrongTarget, &bytes.Buffer{}); result.State != execution.Failed || result.Reason != execution.ReasonUnsupportedOS {
		t.Fatalf("wrong target result=%+v", result)
	}
}

func TestWindowsHostOSIdentityRunnerDescriptorIsCopyIsolated(t *testing.T) {
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{available: execution.ReasonNone})
	descriptor := runner.Descriptor()
	descriptor.Capabilities[0] = "MUTATED"
	descriptor.Requirements.Architectures[0] = "MUTATED"
	if runner.Descriptor().Capabilities[0] != capability.WindowsHostOSIdentitySnapshotID || runner.Descriptor().Requirements.Architectures[0] != "amd64" {
		t.Fatal("descriptor returned mutable internal slices")
	}
}

func TestWindowsHostOSIdentityRunnerAvailabilityAndDescriptor(t *testing.T) {
	runner := newWindowsHostOSIdentityRunner(fakeHostOSIdentityAPI{available: execution.ReasonAPIUnavailable, err: errors.New("missing")})
	if runner.Descriptor().Requirements.Available {
		t.Fatal("unavailable API was declared available")
	}
	if runner.Descriptor().ID != WindowsHostOSIdentityProviderID || runner.Artifact().ContentSchemaID != WindowsHostOSIdentitySchemaID {
		t.Fatalf("descriptor=%+v artifact=%+v", runner.Descriptor(), runner.Artifact())
	}
}

func TestReadBoundedPhysicalDNSHostnameRetriesChangedRequiredSize(t *testing.T) {
	name, err := readBoundedPhysicalDNSHostname(func(buffer []uint16, size *uint32) (bool, error) {
		if len(buffer) == 0 {
			*size = 4
			return false, errors.New("ERROR_MORE_DATA")
		}
		if *size < 8 {
			*size = 8
			return false, errors.New("ERROR_MORE_DATA")
		}
		copy(buffer, utf16.Encode([]rune("HOST")))
		*size = 4
		return true, nil
	})
	if err != nil || name != "HOST" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}

func TestReadBoundedPhysicalDNSHostnameRejectsUnboundedAndNonMoreDataErrors(t *testing.T) {
	if _, err := readBoundedPhysicalDNSHostname(func(_ []uint16, size *uint32) (bool, error) {
		*size = WindowsHostOSIdentityMaxName + 1
		return false, errors.New("ERROR_MORE_DATA")
	}); err == nil {
		t.Fatal("oversized hostname requirement was accepted")
	}
	if _, err := readBoundedPhysicalDNSHostname(func(_ []uint16, _ *uint32) (bool, error) {
		return false, errors.New("ERROR_ACCESS_DENIED")
	}); err == nil {
		t.Fatal("non-more-data hostname failure was accepted")
	}
}

func TestReadBoundedPhysicalDNSHostnameRejectsMalformedUTF16(t *testing.T) {
	for name, units := range map[string][]uint16{
		"lone high surrogate":                {0xD800},
		"lone low surrogate":                 {0xDC00},
		"high surrogate followed by non-low": {0xD800, 'X'},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readBoundedPhysicalDNSHostname(func(buffer []uint16, size *uint32) (bool, error) {
				if len(buffer) == 0 {
					*size = uint32(len(units))
					return false, errors.New("ERROR_MORE_DATA")
				}
				copy(buffer, units)
				*size = uint32(len(units))
				return true, nil
			})
			if err == nil || got != "" {
				t.Fatalf("malformed UTF-16 accepted: name=%q err=%v", got, err)
			}
		})
	}
}

func TestReadBoundedPhysicalDNSHostnameAcceptsSupplementaryPlanePair(t *testing.T) {
	got, err := readBoundedPhysicalDNSHostname(func(buffer []uint16, size *uint32) (bool, error) {
		units := []uint16{0xD83D, 0xDE00}
		if len(buffer) == 0 {
			*size = uint32(len(units))
			return false, errors.New("ERROR_MORE_DATA")
		}
		copy(buffer, units)
		*size = uint32(len(units))
		return true, nil
	})
	if err != nil || got != string(rune(0x1F600)) {
		t.Fatalf("supplementary-plane hostname decoded incorrectly: name=%q err=%v", got, err)
	}
}
