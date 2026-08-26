// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

const (
	WindowsHostOSIdentityProviderID = "windows-native-host-os-identity"
	WindowsHostOSIdentityMediaType  = "application/json"
	WindowsHostOSIdentitySchemaID   = "urn:tannang:artifact:windows-host-os-identity-json-v0"
	WindowsHostOSIdentityMaxName    = 256
)

type hostOSIdentityAPI interface {
	availability() (execution.Reason, error)
	computerNameEx([]uint16, *uint32) (bool, error)
	osVersion() (uint32, uint32, uint32, error)
	nativeArchitecture() (string, error)
}

type windowsHostOSIdentityRunner struct {
	descriptor         Descriptor
	artifact           ArtifactDescriptor
	api                hostOSIdentityAPI
	availabilityReason execution.Reason
	availabilityError  error
}

type hostOSIdentityRecord struct {
	ComputerName       string `json:"computer_name"`
	OSMajor            uint32 `json:"os_major"`
	OSMinor            uint32 `json:"os_minor"`
	OSBuild            uint32 `json:"os_build"`
	NativeArchitecture string `json:"native_architecture"`
}

var _ StreamingRunner = (*windowsHostOSIdentityRunner)(nil)

// NewWindowsHostOSIdentityRunner resolves the native API procedures only.
// It does not read host identity facts during construction.
func NewWindowsHostOSIdentityRunner() StreamingRunner {
	return newWindowsHostOSIdentityRunner(newPlatformHostOSIdentityAPI())
}

func newWindowsHostOSIdentityRunner(api hostOSIdentityAPI) *windowsHostOSIdentityRunner {
	reason := execution.ReasonAPIUnavailable
	var availabilityErr error
	if api != nil {
		reason, availabilityErr = api.availability()
	}
	if !reason.Valid() || (reason == execution.ReasonNone && availabilityErr != nil) {
		reason = execution.ReasonAPIUnavailable
	}
	available := reason == execution.ReasonNone && availabilityErr == nil
	return &windowsHostOSIdentityRunner{
		descriptor: Descriptor{
			ID: WindowsHostOSIdentityProviderID, Class: FirstPartyNative,
			Capabilities: []string{capability.WindowsHostOSIdentitySnapshotID},
			Requirements: Requirements{
				Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"},
				Architectures: []string{"amd64", "x86"}, RequiresElevation: false,
				Available: available, AvailabilityReason: reason,
			},
			SideEffects: []string{
				"Reads the local physical DNS hostname, Windows version/build, and native architecture.",
				"Writes one bounded JSON observation to the caller-owned artifact sink.",
			},
			Quality: Quality{Compatibility: execution.Available, Reason: execution.ReasonNone,
				Fidelity: 4, Disturbance: 1, Completeness: 4, OutputStability: 5, EvidenceValue: 4},
		},
		artifact: ArtifactDescriptor{MediaType: WindowsHostOSIdentityMediaType, ContentSchemaID: WindowsHostOSIdentitySchemaID},
		api:      api, availabilityReason: reason, availabilityError: availabilityErr,
	}
}

func (r *windowsHostOSIdentityRunner) Descriptor() Descriptor {
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

func (r *windowsHostOSIdentityRunner) Artifact() ArtifactDescriptor {
	if r == nil {
		return ArtifactDescriptor{}
	}
	return r.artifact
}

func (r *windowsHostOSIdentityRunner) ExecuteTo(ctx context.Context, request capability.Capability, target fingerprint.TargetFingerprint, writer io.Writer) execution.Result {
	fail := func(reason execution.Reason, detail string) execution.Result {
		return execution.Result{State: execution.Failed, Reason: reason, Detail: detail,
			SideEffectSummary: "Read-only local host identity APIs; incomplete candidate artifact discarded."}
	}
	if err := request.Validate(); err != nil || request.ID != capability.WindowsHostOSIdentitySnapshotID || request.AcquisitionSemantics != capability.StateSnapshot {
		return fail(execution.ReasonProviderError, "Capability does not match WINDOWS_HOST_OS_IDENTITY_SNAPSHOT/STATE_SNAPSHOT")
	}
	if err := target.Validate(); err != nil {
		return fail(execution.ReasonProviderError, "Target Fingerprint is invalid")
	}
	if target.Platform != "windows" || target.OSFamily != "WindowsNT" {
		return fail(execution.ReasonUnsupportedOS, "Windows host identity requires windows/WindowsNT")
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		return fail(execution.ReasonUnsupportedArch, "Windows host identity supports only canonical amd64 and x86 targets")
	}
	if writer == nil {
		return fail(execution.ReasonProviderError, "candidate artifact writer is required")
	}
	if ctx == nil {
		return fail(execution.ReasonProviderError, "execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return fail(execution.ReasonCancelled, "host identity execution was cancelled before observation")
	}
	if r == nil || r.api == nil {
		return fail(execution.ReasonAPIUnavailable, "Windows host identity API is unavailable")
	}
	if r.availabilityReason != execution.ReasonNone {
		return fail(r.availabilityReason, "Windows host identity API is unavailable")
	}
	name, err := readBoundedPhysicalDNSHostname(r.api.computerNameEx)
	if err != nil {
		return fail(execution.ReasonProviderError, "GetComputerNameExW failed")
	}
	major, minor, build, err := r.api.osVersion()
	if err != nil {
		return fail(execution.ReasonProviderError, "RtlGetVersion failed")
	}
	architecture, err := r.api.nativeArchitecture()
	if err != nil {
		return fail(execution.ReasonProviderError, "GetNativeSystemInfo failed")
	}
	if architecture != "x86" && architecture != "amd64" {
		return fail(execution.ReasonProviderError, "native architecture is outside the fixed v0 contract")
	}
	if err := ctx.Err(); err != nil {
		return fail(execution.ReasonCancelled, "host identity execution was cancelled before artifact write")
	}
	encoded, err := json.Marshal(hostOSIdentityRecord{ComputerName: name, OSMajor: major, OSMinor: minor, OSBuild: build, NativeArchitecture: architecture})
	if err != nil {
		return fail(execution.ReasonProviderError, "encode host identity artifact failed")
	}
	if err := writeFull(writer, encoded); err != nil {
		return fail(execution.ReasonProviderError, "write host identity artifact failed")
	}
	if err := ctx.Err(); err != nil {
		return fail(execution.ReasonCancelled, "host identity execution was cancelled after observation")
	}
	return execution.Result{State: execution.Collected, Reason: execution.ReasonNone,
		Detail:            "One bounded Windows host/OS identity JSON observation was written to the caller-owned sink.",
		SideEffectSummary: "Read-only local host identity APIs and one caller-owned artifact write."}
}

func readBoundedPhysicalDNSHostname(query func([]uint16, *uint32) (bool, error)) (string, error) {
	if query == nil {
		return "", errors.New("GetComputerNameExW query is unavailable")
	}
	size := uint32(0)
	if ok, err := query(nil, &size); ok && size == 0 {
		return "", errors.New("GetComputerNameExW returned an empty hostname")
	} else if ok {
		return "", errors.New("GetComputerNameExW returned success without a required size")
	} else if !isMoreDataError(err) {
		return "", err
	}
	if size == 0 || size > WindowsHostOSIdentityMaxName {
		return "", errors.New("GetComputerNameExW required size exceeds the bounded hostname contract")
	}
	for attempt := 0; attempt < 2; attempt++ {
		buffer := make([]uint16, size)
		callSize := size
		ok, err := query(buffer, &callSize)
		if ok {
			if callSize == 0 || callSize > uint32(len(buffer)) {
				return "", errors.New("GetComputerNameExW returned an invalid hostname size")
			}
			name, decodeErr := decodeStrictUTF16(buffer[:callSize])
			if decodeErr != nil {
				return "", decodeErr
			}
			if name == "" {
				return "", errors.New("GetComputerNameExW returned an empty hostname")
			}
			return name, nil
		}
		if !isMoreDataError(err) || attempt == 1 || callSize == 0 || callSize > WindowsHostOSIdentityMaxName {
			return "", err
		}
		size = callSize
	}
	return "", errors.New("GetComputerNameExW required size exceeded bounded retry")
}

// decodeStrictUTF16 validates the native UTF-16 code-unit sequence before
// decoding it. utf16.Decode intentionally replaces malformed surrogate
// sequences; evidence collection must fail closed instead of publishing a
// replacement character as a host name.
func decodeStrictUTF16(units []uint16) (string, error) {
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if index+1 >= len(units) || units[index+1] < 0xDC00 || units[index+1] > 0xDFFF {
				return "", errors.New("GetComputerNameExW returned an unpaired high surrogate")
			}
			index++
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return "", errors.New("GetComputerNameExW returned an unpaired low surrogate")
		}
	}
	return string(utf16.Decode(units)), nil
}

func isMoreDataError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToUpper(err.Error())
	return strings.Contains(text, "MORE_DATA") || strings.Contains(text, "234")
}

func validateHostOSIdentityRecord(record hostOSIdentityRecord) error {
	if strings.TrimSpace(record.ComputerName) == "" {
		return errors.New("computer_name is required")
	}
	if record.NativeArchitecture != "x86" && record.NativeArchitecture != "amd64" {
		return errors.New("native_architecture is invalid")
	}
	return nil
}

func writeHostOSIdentityFull(writer io.Writer, data []byte) error { return writeFull(writer, data) }

func hostOSIdentityOperationError(operation string, err error) error {
	if err == nil {
		return errors.New(operation + " failed")
	}
	return fmt.Errorf("%s failed: %w", operation, err)
}
