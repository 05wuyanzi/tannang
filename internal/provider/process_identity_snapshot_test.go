// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

var errSyntheticProcessAPI = errors.New("synthetic process API failure")

type fakeProcessStep struct {
	ok    bool
	entry processEntry32W
	err   error
	after func()
}

type fakeProcessSnapshotAPI struct {
	availabilityReason execution.Reason
	availabilityErr    error
	createHandle       uintptr
	createErr          error
	afterCreate        func()
	firstStep          fakeProcessStep
	nextSteps          []fakeProcessStep
	closeErr           error

	createCalls int
	firstCalls  int
	nextCalls   int
	closeCalls  int
	entrySizes  []uint32
}

func availableProcessAPI() *fakeProcessSnapshotAPI {
	return &fakeProcessSnapshotAPI{
		availabilityReason: execution.ReasonNone,
		createHandle:       1,
		firstStep:          fakeProcessStep{ok: false},
	}
}

func (api *fakeProcessSnapshotAPI) availability() (execution.Reason, error) {
	return api.availabilityReason, api.availabilityErr
}

func (api *fakeProcessSnapshotAPI) create() (uintptr, error) {
	api.createCalls++
	if api.afterCreate != nil {
		api.afterCreate()
	}
	return api.createHandle, api.createErr
}

func (api *fakeProcessSnapshotAPI) first(_ uintptr, entry *processEntry32W) (bool, error) {
	api.firstCalls++
	api.entrySizes = append(api.entrySizes, entry.Size)
	return applyFakeProcessStep(api.firstStep, entry)
}

func (api *fakeProcessSnapshotAPI) next(_ uintptr, entry *processEntry32W) (bool, error) {
	api.nextCalls++
	api.entrySizes = append(api.entrySizes, entry.Size)
	index := api.nextCalls - 1
	if index >= len(api.nextSteps) {
		return false, nil
	}
	return applyFakeProcessStep(api.nextSteps[index], entry)
}

func (api *fakeProcessSnapshotAPI) close(uintptr) error {
	api.closeCalls++
	return api.closeErr
}

func applyFakeProcessStep(step fakeProcessStep, entry *processEntry32W) (bool, error) {
	if step.after != nil {
		step.after()
	}
	if step.ok {
		size := entry.Size
		*entry = step.entry
		entry.Size = size
	}
	return step.ok, step.err
}

type countingWriter struct {
	bytes.Buffer
	calls int
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	writer.calls++
	return writer.Buffer.Write(value)
}

type shortWriter struct {
	bytes.Buffer
	maximum int
}

func (writer *shortWriter) Write(value []byte) (int, error) {
	count := writer.maximum
	if count > len(value) {
		count = len(value)
	}
	return writer.Buffer.Write(value[:count])
}

type invalidCountWriter struct {
	count func(int) int
	err   error
	calls int
}

func (writer *invalidCountWriter) Write(value []byte) (int, error) {
	writer.calls++
	return writer.count(len(value)), writer.err
}

type failAfterFirstLineWriter struct {
	bytes.Buffer
	calls int
}

func (writer *failAfterFirstLineWriter) Write(value []byte) (int, error) {
	writer.calls++
	if writer.calls == 1 {
		return writer.Buffer.Write(value)
	}
	count := 3
	if count > len(value) {
		count = len(value)
	}
	_, _ = writer.Buffer.Write(value[:count])
	return count, errSyntheticProcessAPI
}

type controlledContext struct {
	context.Context
	mu   sync.RWMutex
	err  error
	done chan struct{}
	once sync.Once
}

func newControlledContext() *controlledContext {
	return &controlledContext{Context: context.Background(), done: make(chan struct{})}
}

func (ctx *controlledContext) Done() <-chan struct{} { return ctx.done }

func (ctx *controlledContext) Err() error {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.err
}

func (ctx *controlledContext) setError(err error) {
	ctx.mu.Lock()
	ctx.err = err
	ctx.mu.Unlock()
	ctx.once.Do(func() { close(ctx.done) })
}

func TestProcessIdentitySnapshotDescriptorArtifactAndIsolation(t *testing.T) {
	t.Parallel()
	runner := newProcessIdentitySnapshotRunner(availableProcessAPI())
	descriptor := runner.Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("Descriptor().Validate() error: %v", err)
	}
	if descriptor.ID != processIdentitySnapshotProviderID || descriptor.Class != FirstPartyNative {
		t.Fatalf("unexpected Provider identity: %+v", descriptor)
	}
	if !descriptor.Supports(capability.ProcessIdentitySnapshotID) {
		t.Fatalf("descriptor does not support %s", capability.ProcessIdentitySnapshotID)
	}
	if !equalStrings(descriptor.Requirements.Platforms, []string{"windows"}) ||
		!equalStrings(descriptor.Requirements.OSFamilies, []string{"WindowsNT"}) ||
		!equalStrings(descriptor.Requirements.Architectures, []string{"amd64", "x86"}) ||
		len(descriptor.Requirements.RuntimeLanes) != 0 || descriptor.Requirements.RequiresElevation ||
		!descriptor.Requirements.Available || descriptor.Requirements.AvailabilityReason != execution.ReasonNone {
		t.Fatalf("unexpected Requirements: %+v", descriptor.Requirements)
	}
	if descriptor.Quality.Compatibility != execution.Available || descriptor.Quality.Reason != execution.ReasonNone ||
		descriptor.Quality.Fidelity != 3 || descriptor.Quality.Disturbance != 1 ||
		descriptor.Quality.Completeness != 2 || descriptor.Quality.OutputStability != 4 ||
		descriptor.Quality.EvidenceValue != 3 {
		t.Fatalf("unexpected Quality: %+v", descriptor.Quality)
	}

	artifact := runner.Artifact()
	if artifact.MediaType != processIdentitySnapshotMediaType || artifact.ContentSchemaID != processIdentitySnapshotSchemaID {
		t.Fatalf("unexpected ArtifactDescriptor: %+v", artifact)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("Artifact().Validate() error: %v", err)
	}
	for _, invalid := range []ArtifactDescriptor{
		{ContentSchemaID: processIdentitySnapshotSchemaID},
		{MediaType: processIdentitySnapshotMediaType},
		{MediaType: " \t", ContentSchemaID: processIdentitySnapshotSchemaID},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid ArtifactDescriptor unexpectedly validated: %+v", invalid)
		}
	}

	descriptor.Capabilities[0] = "MUTATED"
	descriptor.Requirements.Platforms[0] = "mutated"
	descriptor.Requirements.OSFamilies[0] = "mutated"
	descriptor.Requirements.Architectures[0] = "mutated"
	descriptor.Requirements.RuntimeLanes = append(descriptor.Requirements.RuntimeLanes, "MUTATED")
	descriptor.SideEffects[0] = "mutated"
	again := runner.Descriptor()
	if again.Capabilities[0] != capability.ProcessIdentitySnapshotID || again.Requirements.Platforms[0] != "windows" ||
		again.Requirements.OSFamilies[0] != "WindowsNT" || again.Requirements.Architectures[0] != "amd64" ||
		len(again.Requirements.RuntimeLanes) != 0 || again.SideEffects[0] == "mutated" {
		t.Fatalf("Descriptor() retained caller mutations: %+v", again)
	}
}

func TestProcessIdentitySnapshotAvailabilityIsTruthful(t *testing.T) {
	t.Parallel()
	api := availableProcessAPI()
	api.availabilityReason = execution.ReasonAPIUnavailable
	api.availabilityErr = errSyntheticProcessAPI
	runner := newProcessIdentitySnapshotRunner(api)
	descriptor := runner.Descriptor()
	if descriptor.Requirements.Available || descriptor.Requirements.AvailabilityReason != execution.ReasonAPIUnavailable {
		t.Fatalf("unavailable API descriptor = %+v", descriptor.Requirements)
	}
	writer := &countingWriter{}
	result := runner.ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
	requireProcessResult(t, result, execution.Failed, execution.ReasonAPIUnavailable)
	if api.createCalls != 0 || writer.calls != 0 {
		t.Fatalf("unavailable API executed work: create=%d writer=%d", api.createCalls, writer.calls)
	}
}

func TestProcessIdentitySnapshotUnsupportedPlatformAvailabilityBlocks(t *testing.T) {
	t.Parallel()
	api := availableProcessAPI()
	api.availabilityReason = execution.ReasonUnsupportedOS
	api.availabilityErr = errSyntheticProcessAPI
	runner := newProcessIdentitySnapshotRunner(api)
	descriptor := runner.Descriptor()
	if descriptor.Requirements.Available || descriptor.Requirements.AvailabilityReason != execution.ReasonUnsupportedOS {
		t.Fatalf("unsupported-platform descriptor = %+v", descriptor.Requirements)
	}
	writer := &countingWriter{}
	result := runner.ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
	requireProcessResult(t, result, execution.Blocked, execution.ReasonUnsupportedOS)
	if api.createCalls != 0 || writer.calls != 0 {
		t.Fatalf("unsupported platform executed work: create=%d writer=%d", api.createCalls, writer.calls)
	}
}

func TestProcessIdentitySnapshotAcceptsCanonicalTargetArchitectures(t *testing.T) {
	t.Parallel()
	for _, architecture := range []string{"amd64", "x86"} {
		architecture := architecture
		t.Run(architecture, func(t *testing.T) {
			t.Parallel()
			api := availableProcessAPI()
			api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(10, 1, architecture+".exe")}
			runner := newProcessIdentitySnapshotRunner(api)
			writer := &countingWriter{}
			result := runner.ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget(architecture), writer)
			requireProcessResult(t, result, execution.Collected, execution.ReasonNone)
			if api.createCalls != 1 || api.firstCalls != 1 || api.nextCalls != 1 || api.closeCalls != 1 || writer.calls == 0 {
				t.Fatalf("unexpected calls: create=%d first=%d next=%d close=%d writer=%d", api.createCalls, api.firstCalls, api.nextCalls, api.closeCalls, writer.calls)
			}
		})
	}
}

func TestProcessIdentitySnapshotRejectsInvalidContractsBeforeAPIOrWriter(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		request capability.Capability
		target  fingerprint.TargetFingerprint
		state   execution.State
		reason  execution.Reason
	}{
		"invalid capability": {
			request: capability.Capability{}, target: testProcessTarget("amd64"),
			state: execution.Failed, reason: execution.ReasonProviderError,
		},
		"foreign capability": {
			request: foreignProcessCapability(), target: testProcessTarget("amd64"),
			state: execution.Failed, reason: execution.ReasonProviderError,
		},
		"wrong acquisition semantics": {
			request: func() capability.Capability {
				value := capability.ProcessIdentitySnapshot()
				value.AcquisitionSemantics = capability.DerivedDiagnosticReport
				return value
			}(),
			target: testProcessTarget("amd64"), state: execution.Failed, reason: execution.ReasonProviderError,
		},
		"invalid fingerprint": {
			request: capability.ProcessIdentitySnapshot(), target: fingerprint.TargetFingerprint{},
			state: execution.Failed, reason: execution.ReasonProviderError,
		},
		"unsupported operating system": {
			request: capability.ProcessIdentitySnapshot(), target: func() fingerprint.TargetFingerprint {
				value := testProcessTarget("amd64")
				value.Platform = "linux"
				value.OSFamily = "Linux"
				return value
			}(),
			state: execution.Blocked, reason: execution.ReasonUnsupportedOS,
		},
		"unsupported architecture": {
			request: capability.ProcessIdentitySnapshot(), target: testProcessTarget("arm64"),
			state: execution.Blocked, reason: execution.ReasonUnsupportedArch,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := availableProcessAPI()
			runner := newProcessIdentitySnapshotRunner(api)
			writer := &countingWriter{}
			result := runner.ExecuteTo(context.Background(), test.request, test.target, writer)
			requireProcessResult(t, result, test.state, test.reason)
			if api.createCalls != 0 || api.firstCalls != 0 || api.nextCalls != 0 || api.closeCalls != 0 || writer.calls != 0 {
				t.Fatalf("invalid contract crossed execution boundary: api=%+v writer=%d", api, writer.calls)
			}
		})
	}
}

func TestProcessIdentitySnapshotRejectsMissingContextOrWriterBeforeAPI(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		ctx    context.Context
		writer io.Writer
	}{
		"nil context": {writer: &countingWriter{}},
		"nil writer":  {ctx: context.Background()},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := availableProcessAPI()
			result := newProcessIdentitySnapshotRunner(api).ExecuteTo(
				test.ctx,
				capability.ProcessIdentitySnapshot(),
				testProcessTarget("amd64"),
				test.writer,
			)
			requireProcessResult(t, result, execution.Failed, execution.ReasonProviderError)
			if api.createCalls != 0 || api.firstCalls != 0 || api.nextCalls != 0 || api.closeCalls != 0 {
				t.Fatalf("missing invocation contract crossed API boundary: %+v", api)
			}
		})
	}
}

func TestProcessIdentitySnapshotEnumerationOutcomesAndHandleOwnership(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		configure func(*fakeProcessSnapshotAPI)
		state     execution.State
		reason    execution.Reason
		rows      int
		close     int
	}{
		"create failure": {
			configure: func(api *fakeProcessSnapshotAPI) { api.createErr = errSyntheticProcessAPI },
			state:     execution.Failed, reason: execution.ReasonProviderError, close: 0,
		},
		"normal empty snapshot": {
			configure: func(*fakeProcessSnapshotAPI) {},
			state:     execution.Collected, reason: execution.ReasonNone, close: 1,
		},
		"first abnormal failure": {
			configure: func(api *fakeProcessSnapshotAPI) { api.firstStep = fakeProcessStep{err: errSyntheticProcessAPI} },
			state:     execution.Failed, reason: execution.ReasonProviderError, close: 1,
		},
		"next abnormal failure": {
			configure: func(api *fakeProcessSnapshotAPI) {
				api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(7, 0, "first.exe")}
				api.nextSteps = []fakeProcessStep{{err: errSyntheticProcessAPI}}
			},
			state: execution.Partial, reason: execution.ReasonProviderError, rows: 1, close: 1,
		},
		"close failure overrides complete": {
			configure: func(api *fakeProcessSnapshotAPI) {
				api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(7, 0, "first.exe")}
				api.closeErr = errSyntheticProcessAPI
			},
			state: execution.Failed, reason: execution.ReasonProviderError, rows: 1, close: 1,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := availableProcessAPI()
			test.configure(api)
			runner := newProcessIdentitySnapshotRunner(api)
			writer := &countingWriter{}
			result := runner.ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
			requireProcessResult(t, result, test.state, test.reason)
			if api.createCalls != 1 || api.closeCalls != test.close {
				t.Fatalf("handle calls: create=%d close=%d, want 1/%d", api.createCalls, api.closeCalls, test.close)
			}
			if got := len(decodeProcessRecords(t, writer.Bytes())); got != test.rows {
				t.Fatalf("retained complete rows = %d, want %d", got, test.rows)
			}
			if !strings.Contains(result.SideEffectSummary, "complete_rows=") || !strings.Contains(result.SideEffectSummary, "candidate_sink=") {
				t.Fatalf("untruthful side-effect summary: %q", result.SideEffectSummary)
			}
		})
	}
}

func TestProcessIdentitySnapshotPreservesEnumerationOrderAndRecordValues(t *testing.T) {
	t.Parallel()
	fullName := strings.Repeat("x", 260)
	api := availableProcessAPI()
	api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(0, 0, "System Idle Process")}
	api.nextSteps = []fakeProcessStep{
		{ok: true, entry: testProcessEntry(22, 0, "进程😀.exe")},
		{ok: true, entry: testProcessEntry(33, 22, fullName)},
		{ok: false},
	}
	runner := newProcessIdentitySnapshotRunner(api)
	writer := &countingWriter{}
	result := runner.ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
	requireProcessResult(t, result, execution.Collected, execution.ReasonNone)
	records := decodeProcessRecords(t, writer.Bytes())
	want := []processIdentitySnapshotRecord{
		{ProcessID: 0, ParentProcessID: 0, ExecutableName: "System Idle Process"},
		{ProcessID: 22, ParentProcessID: 0, ExecutableName: "进程😀.exe"},
		{ProcessID: 33, ParentProcessID: 22, ExecutableName: fullName},
	}
	if len(records) != len(want) {
		t.Fatalf("record count = %d, want %d", len(records), len(want))
	}
	for index := range want {
		if records[index] != want[index] {
			t.Fatalf("record[%d] = %+v, want %+v", index, records[index], want[index])
		}
	}
	wantSize := uint32(unsafe.Sizeof(processEntry32W{}))
	for index, size := range api.entrySizes {
		if size != wantSize {
			t.Fatalf("entry size[%d] = %d, want %d", index, size, wantSize)
		}
	}
}

func TestStrictExecutableNameDecoding(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("界", 260)
	valid := map[string]struct {
		value [260]uint16
		want  string
	}{
		"ASCII":          {value: testExeFile("test.exe"), want: "test.exe"},
		"BMP Unicode":    {value: testExeFile("进程.exe"), want: "进程.exe"},
		"surrogate pair": {value: testExeFile("😀.exe"), want: "😀.exe"},
		"full field":     {value: testExeFile(full), want: full},
		"stop at NUL":    {value: func() [260]uint16 { value := testExeFile("a.exe"); value[6] = 0xdc00; return value }(), want: "a.exe"},
	}
	for name, test := range valid {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeExecutableName(test.value)
			if err != nil || got != test.want {
				t.Fatalf("decodeExecutableName() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	invalid := map[string][260]uint16{
		"empty":               {},
		"lone high surrogate": {0xd800, 0},
		"lone low surrogate":  {0xdc00, 0},
		"broken pair":         {0xd800, 'x', 0},
	}
	for name, value := range invalid {
		name, value := name, value
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeExecutableName(value)
			if err == nil || strings.Contains(got, "\ufffd") {
				t.Fatalf("malformed UTF-16 decoded as %q, %v", got, err)
			}
		})
	}
}

func TestMalformedExecutableNameStateMapping(t *testing.T) {
	t.Parallel()
	invalid := processEntry32W{ProcessID: 2, ParentProcessID: 1, ExeFile: [260]uint16{0xd800, 0}}
	for name, configure := range map[string]func(*fakeProcessSnapshotAPI){
		"before first retained row": func(api *fakeProcessSnapshotAPI) {
			api.firstStep = fakeProcessStep{ok: true, entry: invalid}
		},
		"after complete row": func(api *fakeProcessSnapshotAPI) {
			api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(1, 0, "first.exe")}
			api.nextSteps = []fakeProcessStep{{ok: true, entry: invalid}}
		},
	} {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := availableProcessAPI()
			configure(api)
			writer := &countingWriter{}
			result := newProcessIdentitySnapshotRunner(api).ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
			wantState := execution.Failed
			wantRows := 0
			if name == "after complete row" {
				wantState = execution.Partial
				wantRows = 1
			}
			requireProcessResult(t, result, wantState, execution.ReasonProviderError)
			if got := len(decodeProcessRecords(t, writer.Bytes())); got != wantRows {
				t.Fatalf("complete rows = %d, want %d", got, wantRows)
			}
		})
	}
}

func TestProcessIdentitySnapshotContextMappings(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		err       error
		configure func(*fakeProcessSnapshotAPI, *controlledContext)
		state     execution.State
		reason    execution.Reason
		rows      int
		create    int
		first     int
		close     int
	}{
		"cancelled before Create": {
			err: context.Canceled, configure: func(*fakeProcessSnapshotAPI, *controlledContext) {},
			state: execution.Failed, reason: execution.ReasonCancelled,
		},
		"cancelled after Create": {
			configure: func(api *fakeProcessSnapshotAPI, ctx *controlledContext) {
				api.afterCreate = func() { ctx.setError(context.Canceled) }
			},
			state: execution.Failed, reason: execution.ReasonCancelled, create: 1, close: 1,
		},
		"cancelled after First before first row": {
			configure: func(api *fakeProcessSnapshotAPI, ctx *controlledContext) {
				api.firstStep = fakeProcessStep{
					ok: true, entry: testProcessEntry(1, 0, "first.exe"),
					after: func() { ctx.setError(context.Canceled) },
				}
			},
			state: execution.Failed, reason: execution.ReasonCancelled, create: 1, first: 1, close: 1,
		},
		"cancelled after complete row": {
			configure: func(api *fakeProcessSnapshotAPI, ctx *controlledContext) {
				api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(1, 0, "first.exe")}
				api.nextSteps = []fakeProcessStep{{ok: true, entry: testProcessEntry(2, 1, "second.exe"), after: func() { ctx.setError(context.Canceled) }}}
			},
			state: execution.Partial, reason: execution.ReasonCancelled, rows: 1, create: 1, first: 1, close: 1,
		},
		"deadline before Create": {
			err: context.DeadlineExceeded, configure: func(*fakeProcessSnapshotAPI, *controlledContext) {},
			state: execution.Failed, reason: execution.ReasonTimeout,
		},
		"deadline after complete row": {
			configure: func(api *fakeProcessSnapshotAPI, ctx *controlledContext) {
				api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(1, 0, "first.exe")}
				api.nextSteps = []fakeProcessStep{{ok: true, entry: testProcessEntry(2, 1, "second.exe"), after: func() { ctx.setError(context.DeadlineExceeded) }}}
			},
			state: execution.Partial, reason: execution.ReasonTimeout, rows: 1, create: 1, first: 1, close: 1,
		},
		"normal exhaustion wins": {
			configure: func(api *fakeProcessSnapshotAPI, ctx *controlledContext) {
				api.firstStep = fakeProcessStep{ok: true, entry: testProcessEntry(1, 0, "first.exe")}
				api.nextSteps = []fakeProcessStep{{ok: false, after: func() { ctx.setError(context.Canceled) }}}
			},
			state: execution.Collected, reason: execution.ReasonNone, rows: 1, create: 1, first: 1, close: 1,
		},
		"unknown context error is Provider error": {
			err: errSyntheticProcessAPI, configure: func(*fakeProcessSnapshotAPI, *controlledContext) {},
			state: execution.Failed, reason: execution.ReasonProviderError,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newControlledContext()
			if test.err != nil {
				ctx.setError(test.err)
			}
			api := availableProcessAPI()
			test.configure(api, ctx)
			writer := &countingWriter{}
			result := newProcessIdentitySnapshotRunner(api).ExecuteTo(ctx, capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
			requireProcessResult(t, result, test.state, test.reason)
			if len(decodeProcessRecords(t, writer.Bytes())) != test.rows {
				t.Fatalf("complete rows = %d, want %d", len(decodeProcessRecords(t, writer.Bytes())), test.rows)
			}
			if api.createCalls != test.create || api.firstCalls != test.first || api.closeCalls != test.close {
				t.Fatalf("calls create/first/close = %d/%d/%d, want %d/%d/%d", api.createCalls, api.firstCalls, api.closeCalls, test.create, test.first, test.close)
			}
		})
	}
}

func TestWriteFullAndWholeSinkDiscardContract(t *testing.T) {
	t.Parallel()
	entry := testProcessEntry(1, 0, "first.exe")
	second := testProcessEntry(2, 1, "second.exe")
	tests := map[string]struct {
		writer func() io.Writer
		rows   int
		state  execution.State
	}{
		"fresh empty bytes buffer": {
			writer: func() io.Writer { return &bytes.Buffer{} }, rows: 1, state: execution.Collected,
		},
		"short nil writes complete": {
			writer: func() io.Writer { return &shortWriter{maximum: 2} }, rows: 1, state: execution.Collected,
		},
		"zero progress": {
			writer: func() io.Writer { return &invalidCountWriter{count: func(int) int { return 0 }} }, state: execution.Failed,
		},
		"partial write plus error": {
			writer: func() io.Writer {
				return &invalidCountWriter{count: func(length int) int {
					if length > 1 {
						return 1
					}
					return length
				}, err: errSyntheticProcessAPI}
			}, state: execution.Failed,
		},
		"full length plus error": {
			writer: func() io.Writer {
				return &invalidCountWriter{count: func(length int) int { return length }, err: errSyntheticProcessAPI}
			}, state: execution.Failed,
		},
		"over reported count": {
			writer: func() io.Writer { return &invalidCountWriter{count: func(length int) int { return length + 1 }} }, state: execution.Failed,
		},
		"negative count": {
			writer: func() io.Writer { return &invalidCountWriter{count: func(int) int { return -1 }} }, state: execution.Failed,
		},
		"failure after complete row": {
			writer: func() io.Writer { return &failAfterFirstLineWriter{} }, rows: 2, state: execution.Failed,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := availableProcessAPI()
			api.firstStep = fakeProcessStep{ok: true, entry: entry}
			if test.rows == 2 {
				api.nextSteps = []fakeProcessStep{{ok: true, entry: second}, {ok: false}}
			}
			writer := test.writer()
			result := newProcessIdentitySnapshotRunner(api).ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
			if test.state == execution.Collected {
				requireProcessResult(t, result, execution.Collected, execution.ReasonNone)
				if got := writerBytes(writer); len(decodeProcessRecords(t, got)) != 1 {
					t.Fatalf("short writer did not produce one complete NDJSON row: %q", got)
				}
				return
			}
			requireProcessResult(t, result, execution.Failed, execution.ReasonProviderError)
			if strings.Contains(result.SideEffectSummary, "candidate_sink=retainable") {
				t.Fatalf("writer failure marked sink retainable: %q", result.SideEffectSummary)
			}
			if name == "failure after complete row" {
				data := writerBytes(writer)
				if bytes.Count(data, []byte{'\n'}) != 1 || len(data) == 0 || data[len(data)-1] == '\n' {
					t.Fatalf("writer failure fixture did not leave one complete row plus a physical partial line: %q", data)
				}
			}
		})
	}
}

func testProcessTarget(architecture string) fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{
		Platform:     "windows",
		OSFamily:     "WindowsNT",
		Version:      "10.0",
		Build:        "26100",
		Architecture: architecture,
		Privilege:    "standard-user",
		RuntimeLane:  "MODERN",
	}
}

func foreignProcessCapability() capability.Capability {
	return capability.Capability{
		ID:                   "FOREIGN_CAPABILITY",
		Description:          "Synthetic foreign capability.",
		AcquisitionSemantics: capability.StateSnapshot,
		Sensitivity:          "low",
	}
}

func testProcessEntry(pid, parent uint32, name string) processEntry32W {
	return processEntry32W{ProcessID: pid, ParentProcessID: parent, ExeFile: testExeFile(name)}
}

func testExeFile(name string) [260]uint16 {
	encoded := utf16.Encode([]rune(name))
	if len(encoded) > 260 {
		panic("test executable name exceeds PROCESSENTRY32W capacity")
	}
	var value [260]uint16
	copy(value[:], encoded)
	return value
}

func decodeProcessRecords(t *testing.T, data []byte) []processIdentitySnapshotRecord {
	t.Helper()
	if len(data) == 0 {
		return nil
	}
	if data[len(data)-1] != '\n' {
		return nil
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	records := make([]processIdentitySnapshotRecord, 0, len(lines))
	for _, line := range lines {
		var record processIdentitySnapshotRecord
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode NDJSON record %q: %v", line, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			t.Fatalf("NDJSON record contains trailing JSON: %q", line)
		}
		records = append(records, record)
	}
	return records
}

func requireProcessResult(t *testing.T, result execution.Result, state execution.State, reason execution.Reason) {
	t.Helper()
	if result.State != state || result.Reason != reason {
		t.Fatalf("result = %+v, want %s/%s", result, state, reason)
	}
	if len(result.Payload) != 0 {
		t.Fatalf("real Provider result carried synthetic Payload: %s", result.Payload)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("result.Validate() error: %v; result=%+v", err, result)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writerBytes(writer io.Writer) []byte {
	switch value := writer.(type) {
	case *bytes.Buffer:
		return value.Bytes()
	case *shortWriter:
		return value.Bytes()
	case *failAfterFirstLineWriter:
		return value.Bytes()
	default:
		return nil
	}
}

var _ context.Context = (*controlledContext)(nil)
