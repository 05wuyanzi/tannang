// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

type fakeEventLogAPI struct {
	availabilityReason execution.Reason
	openErr            error
	exportErr          error
	closeErr           error
	openCalls          int
	exportCalls        int
	closeCalls         int
}

func (f *fakeEventLogAPI) availability() (execution.Reason, error) { return f.availabilityReason, nil }
func (f *fakeEventLogAPI) openLog(channel string) (uintptr, error) {
	f.openCalls++
	if channel != WindowsEventLogSystemChannel {
		return 0, os.ErrInvalid
	}
	if f.openErr != nil {
		return 0, f.openErr
	}
	return 7, nil
}
func (f *fakeEventLogAPI) exportLog(_ string, channel, target string, flags uint32) error {
	f.exportCalls++
	if channel != WindowsEventLogSystemChannel || flags != 1 {
		return os.ErrInvalid
	}
	if f.exportErr != nil {
		return f.exportErr
	}
	return os.WriteFile(target, []byte("EVTX-HEADER-ONLY"), 0o600)
}
func (f *fakeEventLogAPI) close(_ uintptr) error { f.closeCalls++; return f.closeErr }

func testEventTarget() fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "test", Build: "1", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN"}
}

func TestWindowsEventLogSystemDescriptorAndProbeAreFixed(t *testing.T) {
	api := &fakeEventLogAPI{availabilityReason: execution.ReasonNone}
	runner := newWindowsEventLogSystemRunner(api)
	d := runner.Descriptor()
	if d.ID != WindowsEventLogSystemProviderID || d.Class != FirstPartyNative || !d.Supports(capability.WindowsEventLogSystemChannelID) || d.Requirements.Available != true {
		t.Fatalf("descriptor=%+v", d)
	}
	reason, err := runner.Probe(context.Background(), testEventTarget())
	if err != nil || reason != execution.ReasonNone || api.openCalls != 1 || api.closeCalls != 1 {
		t.Fatalf("probe reason=%s err=%v open=%d close=%d", reason, err, api.openCalls, api.closeCalls)
	}
}

func TestWindowsEventLogSystemExportUsesCallerPathAndEmptyChannelIsCollected(t *testing.T) {
	api := &fakeEventLogAPI{availabilityReason: execution.ReasonNone}
	runner := newWindowsEventLogSystemRunner(api)
	target := filepath.Join(t.TempDir(), "system.evtx")
	result := runner.ExecuteToPath(context.Background(), capability.WindowsEventLogSystemChannel(), testEventTarget(), target)
	if result.State != execution.Collected || result.Reason != execution.ReasonNone || api.exportCalls != 1 {
		t.Fatalf("result=%+v calls=%d", result, api.exportCalls)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "EVTX-HEADER-ONLY" {
		t.Fatalf("exported bytes=%q err=%v", data, err)
	}
}

func TestWindowsEventLogSystemAccessDeniedIsBounded(t *testing.T) {
	api := &fakeEventLogAPI{availabilityReason: execution.ReasonNone, openErr: &eventLogFailure{reason: execution.ReasonPrivilegeRequired, err: os.ErrPermission}, exportErr: &eventLogFailure{reason: execution.ReasonPrivilegeRequired, err: os.ErrPermission}}
	runner := newWindowsEventLogSystemRunner(api)
	reason, err := runner.Probe(context.Background(), testEventTarget())
	if reason != execution.ReasonPrivilegeRequired || err == nil {
		t.Fatalf("reason=%s err=%v", reason, err)
	}
	result := runner.ExecuteToPath(context.Background(), capability.WindowsEventLogSystemChannel(), testEventTarget(), filepath.Join(t.TempDir(), "system.evtx"))
	if result.State != execution.Blocked || result.Reason != execution.ReasonPrivilegeRequired {
		t.Fatalf("execution=%+v", result)
	}
}
