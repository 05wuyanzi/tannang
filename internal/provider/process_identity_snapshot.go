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
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

const (
	processIdentitySnapshotProviderID = "windows-toolhelp-process-snapshot"
	processIdentitySnapshotMediaType  = "application/x-ndjson"
	processIdentitySnapshotSchemaID   = "https://github.com/05wuyanzi/tannang/contracts/process-identity-snapshot-record-v0.schema.json"
)

type processEntry32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type processSnapshotAPI interface {
	availability() (execution.Reason, error)
	create() (uintptr, error)
	first(uintptr, *processEntry32W) (bool, error)
	next(uintptr, *processEntry32W) (bool, error)
	close(uintptr) error
}

type processIdentitySnapshotRunner struct {
	descriptor         Descriptor
	artifact           ArtifactDescriptor
	api                processSnapshotAPI
	availabilityReason execution.Reason
	availabilityError  error
}

var _ StreamingRunner = (*processIdentitySnapshotRunner)(nil)

type processIdentitySnapshotRecord struct {
	ProcessID       uint32 `json:"process_id"`
	ParentProcessID uint32 `json:"parent_process_id"`
	ExecutableName  string `json:"executable_name"`
}

type processOutcome struct {
	state  execution.State
	reason execution.Reason
	detail string
	rows   int
}

type processExecutionFacts struct {
	snapshotCreated bool
	closeOutcome    string
}

// NewProcessIdentitySnapshotRunner constructs the explicit library-level
// Tool Help Provider. Construction may resolve procedures but never creates a
// process snapshot.
func NewProcessIdentitySnapshotRunner() StreamingRunner {
	return newProcessIdentitySnapshotRunner(newPlatformProcessSnapshotAPI())
}

func newProcessIdentitySnapshotRunner(api processSnapshotAPI) *processIdentitySnapshotRunner {
	reason := execution.ReasonAPIUnavailable
	var availabilityErr error
	if api != nil {
		reason, availabilityErr = api.availability()
	}
	if !reason.Valid() || (reason == execution.ReasonNone && availabilityErr != nil) {
		reason = execution.ReasonAPIUnavailable
	}
	available := reason == execution.ReasonNone && availabilityErr == nil
	return &processIdentitySnapshotRunner{
		descriptor: Descriptor{
			ID:           processIdentitySnapshotProviderID,
			Class:        FirstPartyNative,
			Capabilities: []string{capability.ProcessIdentitySnapshotID},
			Requirements: Requirements{
				Platforms:          []string{"windows"},
				OSFamilies:         []string{"WindowsNT"},
				Architectures:      []string{"amd64", "x86"},
				RuntimeLanes:       []string{},
				RequiresElevation:  false,
				Available:          available,
				AvailabilityReason: reason,
			},
			SideEffects: []string{
				"Creates one read-only Tool Help process snapshot kernel handle.",
				"Sequentially reads process ID, parent process ID, and executable name.",
				"Writes serialized observations only to the caller-owned artifact sink.",
			},
			Quality: Quality{
				Compatibility:   execution.Available,
				Reason:          execution.ReasonNone,
				Fidelity:        3,
				Disturbance:     1,
				Completeness:    2,
				OutputStability: 4,
				EvidenceValue:   3,
			},
		},
		artifact: ArtifactDescriptor{
			MediaType:       processIdentitySnapshotMediaType,
			ContentSchemaID: processIdentitySnapshotSchemaID,
		},
		api:                api,
		availabilityReason: reason,
		availabilityError:  availabilityErr,
	}
}

// Descriptor returns a copy-isolated declaration.
func (r *processIdentitySnapshotRunner) Descriptor() Descriptor {
	descriptor := r.descriptor
	descriptor.Capabilities = append([]string(nil), r.descriptor.Capabilities...)
	descriptor.Requirements.Platforms = append([]string(nil), r.descriptor.Requirements.Platforms...)
	descriptor.Requirements.OSFamilies = append([]string(nil), r.descriptor.Requirements.OSFamilies...)
	descriptor.Requirements.Architectures = append([]string(nil), r.descriptor.Requirements.Architectures...)
	descriptor.Requirements.RuntimeLanes = append([]string(nil), r.descriptor.Requirements.RuntimeLanes...)
	descriptor.SideEffects = append([]string(nil), r.descriptor.SideEffects...)
	return descriptor
}

// Artifact returns the immediate NDJSON record contract.
func (r *processIdentitySnapshotRunner) Artifact() ArtifactDescriptor {
	return r.artifact
}

// ExecuteTo synchronously enumerates one Tool Help process snapshot. The
// caller must provide a fresh, empty, exclusive, single-use, uncommitted, and
// wholly discardable candidate sink. The Provider never closes, flushes,
// truncates, hashes, publishes, retains, or reuses the writer.
func (r *processIdentitySnapshotRunner) ExecuteTo(
	ctx context.Context,
	request capability.Capability,
	target fingerprint.TargetFingerprint,
	writer io.Writer,
) execution.Result {
	facts := processExecutionFacts{closeOutcome: "not-applicable"}
	if r == nil || r.api == nil {
		return processResult(processOutcome{
			state: execution.Failed, reason: execution.ReasonProviderError,
			detail: "process snapshot Provider is not initialized",
		}, facts)
	}
	if err := request.Validate(); err != nil {
		return processResult(providerFailure(0, operationDetail("validate Capability", err)), facts)
	}
	if request.ID != capability.ProcessIdentitySnapshotID || request.AcquisitionSemantics != capability.StateSnapshot {
		return processResult(providerFailure(0, "Capability does not match PROCESS_IDENTITY_SNAPSHOT/STATE_SNAPSHOT"), facts)
	}
	if err := target.Validate(); err != nil {
		return processResult(providerFailure(0, operationDetail("validate Target Fingerprint", err)), facts)
	}
	if target.Platform != "windows" || target.OSFamily != "WindowsNT" {
		return processResult(processOutcome{
			state: execution.Blocked, reason: execution.ReasonUnsupportedOS,
			detail: "PROCESS_IDENTITY_SNAPSHOT requires windows/WindowsNT",
		}, facts)
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		return processResult(processOutcome{
			state: execution.Blocked, reason: execution.ReasonUnsupportedArch,
			detail: "PROCESS_IDENTITY_SNAPSHOT supports only canonical target architectures amd64 and x86",
		}, facts)
	}
	if r.availabilityReason != execution.ReasonNone {
		if r.availabilityReason == execution.ReasonUnsupportedOS {
			return processResult(processOutcome{
				state: execution.Blocked, reason: execution.ReasonUnsupportedOS,
				detail: operationDetail("process snapshot API is unavailable on this platform", r.availabilityError),
			}, facts)
		}
		return processResult(processOutcome{
			state: execution.Failed, reason: execution.ReasonAPIUnavailable,
			detail: operationDetail("Tool Help process snapshot API is unavailable", r.availabilityError),
		}, facts)
	}
	if writer == nil {
		return processResult(providerFailure(0, "candidate artifact writer is required"), facts)
	}
	if ctx == nil {
		return processResult(providerFailure(0, "execution context is required"), facts)
	}
	if outcome, stopped := contextOutcome(ctx.Err(), 0); stopped {
		return processResult(outcome, facts)
	}

	handle, err := r.api.create()
	if err != nil {
		return processResult(providerFailure(0, operationDetail("CreateToolhelp32Snapshot", err)), facts)
	}
	facts.snapshotCreated = true
	facts.closeOutcome = "pending"
	outcome := r.enumerate(ctx, handle, writer)
	if err := r.api.close(handle); err != nil {
		facts.closeOutcome = "failed"
		outcome = processOutcome{
			state: execution.Failed, reason: execution.ReasonProviderError,
			detail: operationDetail("CloseHandle", err), rows: outcome.rows,
		}
	} else {
		facts.closeOutcome = "succeeded"
	}
	return processResult(outcome, facts)
}

func (r *processIdentitySnapshotRunner) enumerate(ctx context.Context, handle uintptr, writer io.Writer) processOutcome {
	if outcome, stopped := contextOutcome(ctx.Err(), 0); stopped {
		return outcome
	}
	var entry processEntry32W
	prepareProcessEntry(&entry)
	ok, err := r.api.first(handle, &entry)
	if err != nil {
		return providerFailure(0, operationDetail("Process32FirstW", err))
	}
	if !ok {
		return processOutcome{state: execution.Collected, reason: execution.ReasonNone}
	}

	rows := 0
	for {
		if outcome, stopped := contextOutcome(ctx.Err(), rows); stopped {
			return outcome
		}
		name, err := decodeExecutableName(entry.ExeFile)
		if err != nil {
			return providerFailure(rows, operationDetail("decode PROCESSENTRY32W.szExeFile", err))
		}
		line, err := json.Marshal(processIdentitySnapshotRecord{
			ProcessID:       entry.ProcessID,
			ParentProcessID: entry.ParentProcessID,
			ExecutableName:  name,
		})
		if err != nil {
			return providerFailure(rows, operationDetail("encode process identity record", err))
		}
		line = append(line, '\n')
		if outcome, stopped := contextOutcome(ctx.Err(), rows); stopped {
			return outcome
		}
		if err := writeFull(writer, line); err != nil {
			return processOutcome{
				state: execution.Failed, reason: execution.ReasonProviderError,
				detail: operationDetail("write process identity record", err), rows: rows,
			}
		}
		rows++
		if outcome, stopped := contextOutcome(ctx.Err(), rows); stopped {
			return outcome
		}

		prepareProcessEntry(&entry)
		ok, err = r.api.next(handle, &entry)
		if err != nil {
			return providerFailure(rows, operationDetail("Process32NextW", err))
		}
		if !ok {
			return processOutcome{state: execution.Collected, reason: execution.ReasonNone, rows: rows}
		}
	}
}

func prepareProcessEntry(entry *processEntry32W) {
	*entry = processEntry32W{}
	entry.Size = uint32(unsafe.Sizeof(*entry))
}

func decodeExecutableName(value [260]uint16) (string, error) {
	end := len(value)
	for index, unit := range value {
		if unit == 0 {
			end = index
			break
		}
	}
	if end == 0 {
		return "", errors.New("executable name is empty")
	}
	runes := make([]rune, 0, end)
	for index := 0; index < end; index++ {
		unit := rune(value[index])
		switch {
		case unit >= 0xd800 && unit <= 0xdbff:
			if index+1 >= end {
				return "", errors.New("executable name contains an unpaired high surrogate")
			}
			low := rune(value[index+1])
			if low < 0xdc00 || low > 0xdfff {
				return "", errors.New("executable name contains a broken surrogate pair")
			}
			runes = append(runes, utf16.DecodeRune(unit, low))
			index++
		case unit >= 0xdc00 && unit <= 0xdfff:
			return "", errors.New("executable name contains an unpaired low surrogate")
		default:
			runes = append(runes, unit)
		}
	}
	if len(runes) == 0 {
		return "", errors.New("executable name is empty")
	}
	return string(runes), nil
}

func writeFull(writer io.Writer, remaining []byte) error {
	for len(remaining) > 0 {
		n, err := writer.Write(remaining)
		if n < 0 || n > len(remaining) {
			return fmt.Errorf("writer returned invalid byte count %d for %d remaining bytes", n, len(remaining))
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		remaining = remaining[n:]
	}
	return nil
}

func providerFailure(rows int, detail string) processOutcome {
	state := execution.Failed
	if rows > 0 {
		state = execution.Partial
	}
	return processOutcome{state: state, reason: execution.ReasonProviderError, detail: detail, rows: rows}
}

func contextOutcome(err error, rows int) (processOutcome, bool) {
	if err == nil {
		return processOutcome{}, false
	}
	state := execution.Failed
	if rows > 0 {
		state = execution.Partial
	}
	switch {
	case errors.Is(err, context.Canceled):
		return processOutcome{
			state: state, reason: execution.ReasonCancelled,
			detail: "process snapshot execution was explicitly cancelled by the caller", rows: rows,
		}, true
	case errors.Is(err, context.DeadlineExceeded):
		return processOutcome{
			state: state, reason: execution.ReasonTimeout,
			detail: "process snapshot execution deadline expired", rows: rows,
		}, true
	default:
		return processOutcome{
			state: state, reason: execution.ReasonProviderError,
			detail: operationDetail("process snapshot context ended", err), rows: rows,
		}, true
	}
}

func processResult(outcome processOutcome, facts processExecutionFacts) execution.Result {
	disposition := "discard"
	if outcome.state == execution.Collected || outcome.state == execution.Partial {
		disposition = "retainable"
	}
	return execution.Result{
		State:  outcome.state,
		Reason: outcome.reason,
		Detail: outcome.detail,
		SideEffectSummary: fmt.Sprintf(
			"Tool Help process snapshot created=%t; close=%s; complete_rows=%d; candidate_sink=%s; process_handles_opened=0; commands_executed=0; system_modified=false.",
			facts.snapshotCreated,
			facts.closeOutcome,
			outcome.rows,
			disposition,
		),
	}
}

func operationDetail(operation string, err error) string {
	detail := strings.TrimSpace(operation)
	if err != nil {
		detail += ": " + err.Error()
	}
	const maxRunes = 512
	runes := []rune(detail)
	if len(runes) > maxRunes {
		detail = string(runes[:maxRunes])
	}
	return detail
}
