// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

const (
	WindowsEventLogSystemProviderID = "windows-wevtapi-system-channel"
	WindowsEventLogSystemMediaType  = "application/x-evtx"
	WindowsEventLogSystemSchemaID   = "urn:tannang:artifact:windows-event-log-system-evtx-v0"
	WindowsEventLogSystemChannel    = "System"
	evtExportLogChannelPath         = uint32(0x1)
)

// AvailabilityProber is an optional run-scoped, read-only compatibility seam.
// It must not read event contents or create a retained artifact.
type AvailabilityProber interface {
	Probe(context.Context, fingerprint.TargetFingerprint) (execution.Reason, error)
}

type eventLogAPI interface {
	availability() (execution.Reason, error)
	openLog(string) (uintptr, error)
	exportLog(string, string, string, uint32) error
	close(uintptr) error
}

type windowsEventLogSystemRunner struct {
	descriptor         Descriptor
	artifact           ArtifactDescriptor
	api                eventLogAPI
	availabilityReason execution.Reason
	availabilityError  error
}

var _ FileArtifactRunner = (*windowsEventLogSystemRunner)(nil)
var _ AvailabilityProber = (*windowsEventLogSystemRunner)(nil)

// NewWindowsEventLogSystemRunner constructs the fixed native Provider. It
// resolves only the native API and does not open or export a channel.
func NewWindowsEventLogSystemRunner() FileArtifactRunner {
	return newWindowsEventLogSystemRunner(newPlatformEventLogAPI())
}

func newWindowsEventLogSystemRunner(api eventLogAPI) *windowsEventLogSystemRunner {
	reason := execution.ReasonAPIUnavailable
	var availabilityErr error
	if api != nil {
		reason, availabilityErr = api.availability()
	}
	if !reason.Valid() || (reason == execution.ReasonNone && availabilityErr != nil) {
		reason = execution.ReasonAPIUnavailable
	}
	available := reason == execution.ReasonNone && availabilityErr == nil
	return &windowsEventLogSystemRunner{
		descriptor: Descriptor{
			ID:           WindowsEventLogSystemProviderID,
			Class:        FirstPartyNative,
			Capabilities: []string{capability.WindowsEventLogSystemChannelID},
			Requirements: Requirements{
				Platforms:          []string{"windows"},
				OSFamilies:         []string{"WindowsNT"},
				Architectures:      []string{"amd64", "x86"},
				RuntimeLanes:       nil,
				RequiresElevation:  false,
				Available:          available,
				AvailabilityReason: reason,
			},
			SideEffects: []string{
				"Reads the local retained Windows System Event Log channel.",
				"Writes one native EVTX export to the caller-owned staging path.",
			},
			Quality: Quality{
				Compatibility:   execution.Available,
				Reason:          execution.ReasonNone,
				Fidelity:        5,
				Disturbance:     1,
				Completeness:    5,
				OutputStability: 5,
				EvidenceValue:   5,
			},
		},
		artifact: ArtifactDescriptor{
			MediaType:       WindowsEventLogSystemMediaType,
			ContentSchemaID: WindowsEventLogSystemSchemaID,
		},
		api:                api,
		availabilityReason: reason,
		availabilityError:  availabilityErr,
	}
}

func (r *windowsEventLogSystemRunner) Descriptor() Descriptor {
	if r == nil {
		return Descriptor{}
	}
	d := r.descriptor
	d.Capabilities = append([]string(nil), d.Capabilities...)
	d.Requirements.Platforms = append([]string(nil), d.Requirements.Platforms...)
	d.Requirements.OSFamilies = append([]string(nil), d.Requirements.OSFamilies...)
	d.Requirements.Architectures = append([]string(nil), d.Requirements.Architectures...)
	d.Requirements.RuntimeLanes = append([]string(nil), d.Requirements.RuntimeLanes...)
	d.SideEffects = append([]string(nil), d.SideEffects...)
	return d
}

func (r *windowsEventLogSystemRunner) Artifact() ArtifactDescriptor {
	if r == nil {
		return ArtifactDescriptor{}
	}
	return r.artifact
}

// Probe performs a bounded local fixed-channel open/close check. It never
// reads event contents and it never accepts an operator-provided channel.
func (r *windowsEventLogSystemRunner) Probe(ctx context.Context, target fingerprint.TargetFingerprint) (execution.Reason, error) {
	if ctx == nil {
		return execution.ReasonProviderError, errors.New("event log probe context is required")
	}
	if err := ctx.Err(); err != nil {
		return execution.ReasonCancelled, err
	}
	if err := target.Validate(); err != nil {
		return execution.ReasonProviderError, err
	}
	if target.Platform != "windows" || target.OSFamily != "WindowsNT" {
		return execution.ReasonUnsupportedOS, nil
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		return execution.ReasonUnsupportedArch, nil
	}
	if r == nil || r.api == nil {
		return execution.ReasonAPIUnavailable, errors.New("Windows Event Log API is unavailable")
	}
	if r.availabilityReason != execution.ReasonNone {
		return r.availabilityReason, r.availabilityError
	}
	handle, err := r.api.openLog(WindowsEventLogSystemChannel)
	if err != nil {
		return eventLogReason(err), err
	}
	if err := r.api.close(handle); err != nil {
		return eventLogReason(err), err
	}
	return execution.ReasonNone, nil
}

func (r *windowsEventLogSystemRunner) ExecuteToPath(ctx context.Context, request capability.Capability, target fingerprint.TargetFingerprint, absoluteStagingPath string) execution.Result {
	if err := request.Validate(); err != nil || request.ID != capability.WindowsEventLogSystemChannelID || request.AcquisitionSemantics != capability.ExistingArtifactExport {
		return eventLogResult(execution.Failed, execution.ReasonProviderError, "Capability does not match WINDOWS_EVENT_LOG_SYSTEM_CHANNEL/EXISTING_ARTIFACT_EXPORT")
	}
	if err := target.Validate(); err != nil {
		return eventLogResult(execution.Failed, execution.ReasonProviderError, "Target Fingerprint is invalid")
	}
	if target.Platform != "windows" || target.OSFamily != "WindowsNT" {
		return eventLogResult(execution.Blocked, execution.ReasonUnsupportedOS, "Windows Event Log requires windows/WindowsNT")
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		return eventLogResult(execution.Blocked, execution.ReasonUnsupportedArch, "Windows Event Log supports only canonical amd64 and x86 targets")
	}
	if strings.TrimSpace(absoluteStagingPath) == "" || !filepath.IsAbs(absoluteStagingPath) {
		return eventLogResult(execution.Failed, execution.ReasonProviderError, "caller-owned absolute staging path is required")
	}
	if ctx == nil {
		return eventLogResult(execution.Failed, execution.ReasonProviderError, "execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return eventLogResult(execution.Skipped, execution.ReasonCancelled, "Event Log export was cancelled before execution")
	}
	if r == nil || r.api == nil {
		return eventLogResult(execution.Failed, execution.ReasonAPIUnavailable, "Windows Event Log API is unavailable")
	}
	if r.availabilityReason != execution.ReasonNone {
		return eventLogResult(execution.Failed, r.availabilityReason, "Windows Event Log API is unavailable")
	}
	if err := r.api.exportLog("", WindowsEventLogSystemChannel, absoluteStagingPath, evtExportLogChannelPath); err != nil {
		if reason := eventLogReason(err); reason == execution.ReasonPrivilegeRequired {
			return eventLogResult(execution.Blocked, reason, "access to the Windows System Event Log was denied")
		}
		return eventLogResult(execution.Failed, execution.ReasonProviderError, "Windows System Event Log export failed")
	}
	return eventLogResult(execution.Collected, execution.ReasonNone, "One native Windows System Event Log EVTX export was written to the caller-owned staging path")
}

func eventLogResult(state execution.State, reason execution.Reason, detail string) execution.Result {
	return execution.Result{State: state, Reason: reason, Detail: detail, SideEffectSummary: "Read-only local Event Log access and one caller-owned artifact write."}
}

// eventLogFailure is used by the native and fake APIs to preserve bounded
// classification without exposing raw Win32 diagnostics to operators.
type eventLogFailure struct {
	reason execution.Reason
	err    error
}

func (e *eventLogFailure) Error() string {
	if e == nil || e.err == nil {
		return "Event Log operation failed"
	}
	return e.err.Error()
}
func (e *eventLogFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func eventLogReason(err error) execution.Reason {
	var failure *eventLogFailure
	if errors.As(err, &failure) && failure != nil && failure.reason.Valid() {
		return failure.reason
	}
	return execution.ReasonProviderError
}
