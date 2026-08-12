// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

const testCollectionID = "COL-00000000-0000-4000-8000-000000000001"

type fakeRunner struct {
	descriptor provider.Descriptor
	result     execution.Result
	execute    func(context.Context, capability.Capability, fingerprint.TargetFingerprint) execution.Result
	calls      atomic.Int32
	active     atomic.Int32
	maximum    atomic.Int32
}

func (f *fakeRunner) Descriptor() provider.Descriptor { return f.descriptor }

func (f *fakeRunner) Execute(ctx context.Context, request capability.Capability, target fingerprint.TargetFingerprint) execution.Result {
	f.calls.Add(1)
	active := f.active.Add(1)
	for {
		maximum := f.maximum.Load()
		if active <= maximum || f.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	defer f.active.Add(-1)
	if f.execute != nil {
		return f.execute(ctx, request, target)
	}
	return cloneExecutionResult(f.result)
}

type fakeFinalizer struct {
	finalize func(context.Context, CollectionContext, fingerprint.TargetFingerprint, []CapabilityRecord) (FinalizationResult, error)
	calls    atomic.Int32
}

func (f *fakeFinalizer) Finalize(
	ctx context.Context,
	collection CollectionContext,
	target fingerprint.TargetFingerprint,
	records []CapabilityRecord,
) (FinalizationResult, error) {
	f.calls.Add(1)
	if f.finalize != nil {
		return f.finalize(ctx, collection, target, records)
	}
	return successfulFinalization(records), nil
}

func TestNewFirstStageRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*FirstStageConfig){
		"empty baseline":       func(config *FirstStageConfig) { config.ProtectedBaseline = nil },
		"missing seam":         func(config *FirstStageConfig) { config.StartupPrerequisite = nil },
		"non-positive timeout": func(config *FirstStageConfig) { config.FinalizationTimeout = 0 },
		"unknown baseline": func(config *FirstStageConfig) {
			config.ProtectedBaseline[0].ID = "SYNTHETIC_UNKNOWN"
		},
		"unprotected baseline": func(config *FirstStageConfig) { config.ProtectedBaseline[0].Protected = false },
		"duplicate capability": func(config *FirstStageConfig) {
			config.CapabilityCatalog = append(config.CapabilityCatalog, config.CapabilityCatalog[0])
		},
		"duplicate provider": func(config *FirstStageConfig) {
			config.Providers = append(config.Providers, config.Providers[0])
		},
		"non-synthetic provider": func(config *FirstStageConfig) {
			runner := config.Providers[0].(*fakeRunner)
			replacement := newFakeRunner(runner.descriptor.ID, runner.descriptor.Capabilities, runner.result)
			replacement.descriptor.Class = provider.WindowsInbox
			config.Providers = []provider.Runner{replacement}
		},
		"active trace baseline": func(config *FirstStageConfig) {
			config.CapabilityCatalog[0].AcquisitionSemantics = capability.ActiveTrace
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := testConfig()
			mutate(&config)
			if _, err := NewFirstStage(config); err == nil {
				t.Fatal("NewFirstStage() unexpectedly accepted invalid configuration")
			}
		})
	}
}

func TestRunRequestAndCaseIDBoundaries(t *testing.T) {
	t.Parallel()
	for name, request := range map[string]RunRequest{
		"empty output": {},
		"supplemental protected": {
			OutputDestination: `C:\output`,
			Supplemental:      []capability.CapabilityRequest{{ID: "SYNTHETIC_EXTRA", Priority: capability.PriorityNormal, Protected: true}},
		},
		"invalid supplemental id": {
			OutputDestination: `C:\output`,
			Supplemental:      []capability.CapabilityRequest{{ID: "bad-id", Priority: capability.PriorityNormal}},
		},
		"control in case id":    {OutputDestination: `C:\output`, CaseID: "case\ninvalid"},
		"whitespace case id":    {OutputDestination: `C:\output`, CaseID: "   "},
		"long case id":          {OutputDestination: `C:\output`, CaseID: strings.Repeat("x", 129)},
		"invalid UTF-8 case id": {OutputDestination: `C:\output`, CaseID: string([]byte{0xff})},
	} {
		if err := request.Validate(); err == nil {
			t.Fatalf("%s unexpectedly validated", name)
		}
	}
	if err := (RunRequest{OutputDestination: `C:\output`, CaseID: "CASE-01"}).Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := (RunRequest{OutputDestination: `C:\output`, CaseID: strings.Repeat("界", 128)}).Validate(); err != nil {
		t.Fatalf("128-character case ID rejected: %v", err)
	}
}

func TestProtectedBaselineMergeIsDeterministic(t *testing.T) {
	t.Parallel()
	baseline := []capability.CapabilityRequest{
		{ID: "SYNTHETIC_Z", Priority: capability.PriorityNormal, Protected: true},
		{ID: "SYNTHETIC_B", Priority: capability.PriorityEarly, Protected: true},
	}
	supplemental := []capability.CapabilityRequest{
		{ID: "SYNTHETIC_Z", Priority: capability.PriorityEarly},
		{ID: "SYNTHETIC_A", Priority: capability.PriorityEarly},
		{ID: "SYNTHETIC_M", Priority: capability.PriorityNormal},
		{ID: "SYNTHETIC_M", Priority: capability.PriorityLate},
	}
	got := mergeRequests(baseline, supplemental)
	want := []string{"SYNTHETIC_B", "SYNTHETIC_Z", "SYNTHETIC_A", "SYNTHETIC_M"}
	if len(got) != len(want) {
		t.Fatalf("merged request count = %d, want %d", len(got), len(want))
	}
	for index, id := range want {
		if got[index].ID != id {
			t.Fatalf("merged order[%d] = %s, want %s", index, got[index].ID, id)
		}
	}
	if !got[1].Protected || got[1].Priority != capability.PriorityEarly {
		t.Fatalf("protected duplicate lost protection or earliest priority: %+v", got[1])
	}
}

func TestCollectionIDGenerationAndNoCompletedRunHistory(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	for index := 0; index < 32; index++ {
		value, err := NewCollectionID()
		if err != nil {
			t.Fatal(err)
		}
		if err := validateCollectionID(value); err != nil {
			t.Fatalf("generated ID %q is invalid: %v", value, err)
		}
		if value[18] != '4' || !strings.ContainsRune("89ab", rune(value[23])) {
			t.Fatalf("generated ID %q has incorrect UUIDv4 version/variant text", value)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate generated collection id %q", value)
		}
		seen[value] = struct{}{}
	}

	config := testConfig()
	config.GenerateCollectionID = func() (string, error) { return "not-a-uuid", nil }
	stage, err := NewFirstStage(config)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := stage.Run(context.Background(), validRunRequest()); err == nil || result.Context.CollectionID != "" {
		t.Fatalf("invalid generated ID result=%+v error=%v", result, err)
	}

	config = testConfig()
	stage, err = NewFirstStage(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Run(context.Background(), validRunRequest()); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if _, err := stage.Run(context.Background(), validRunRequest()); err != nil {
		t.Fatalf("trusted deterministic generator was rejected after completed Run: %v", err)
	}
}

func TestSameInstanceOverlappingRunIsRejectedAndGateIsReleased(t *testing.T) {
	t.Parallel()
	config := testConfig()
	firstEnteredStartup := make(chan struct{})
	releaseFirst := make(chan struct{})
	var idCalls atomic.Int32
	var startupCalls atomic.Int32
	var outputCalls atomic.Int32
	var fingerprintCalls atomic.Int32
	var resolverCalls atomic.Int32

	config.GenerateCollectionID = func() (string, error) {
		idCalls.Add(1)
		return testCollectionID, nil
	}
	config.StartupPrerequisite = func(context.Context, CollectionContext) error {
		if startupCalls.Add(1) == 1 {
			close(firstEnteredStartup)
			<-releaseFirst
		}
		return nil
	}
	config.OutputPathPrerequisite = func(string) error {
		outputCalls.Add(1)
		return nil
	}
	config.FingerprintProbe = func(context.Context, string) (fingerprint.TargetFingerprint, error) {
		fingerprintCalls.Add(1)
		return testFingerprint(), nil
	}
	baseResolve := config.Resolve
	config.Resolve = func(request capability.Capability, target fingerprint.TargetFingerprint, candidates []provider.Descriptor, policy resolver.Policy) (resolver.Decision, error) {
		resolverCalls.Add(1)
		return baseResolve(request, target, candidates, policy)
	}
	runner := config.Providers[0].(*fakeRunner)
	finalizer := config.Finalizer.(*fakeFinalizer)
	stage := mustFirstStage(t, config)

	type runOutcome struct {
		result RunResult
		err    error
	}
	firstDone := make(chan runOutcome, 1)
	go func() {
		result, err := stage.Run(context.Background(), validRunRequest())
		firstDone <- runOutcome{result: result, err: err}
	}()
	select {
	case <-firstEnteredStartup:
	case <-time.After(time.Second):
		t.Fatal("first Run did not enter the startup seam")
	}

	secondDone := make(chan runOutcome, 1)
	go func() {
		result, err := stage.Run(context.Background(), validRunRequest())
		secondDone <- runOutcome{result: result, err: err}
	}()
	var second runOutcome
	select {
	case second = <-secondDone:
	case <-time.After(time.Second):
		close(releaseFirst)
		<-firstDone
		t.Fatal("overlapping Run was queued instead of rejected promptly")
	}
	if second.err == nil || second.result.Context.CollectionID != "" || len(second.result.Records) != 0 {
		t.Fatalf("overlapping Run result=%+v error=%v", second.result, second.err)
	}
	if idCalls.Load() != 1 || startupCalls.Load() != 1 || outputCalls.Load() != 0 || fingerprintCalls.Load() != 0 ||
		resolverCalls.Load() != 0 || runner.calls.Load() != 0 || finalizer.calls.Load() != 0 {
		t.Fatalf("overlapping Run invoked a seam: id=%d startup=%d output=%d fingerprint=%d resolver=%d provider=%d finalizer=%d",
			idCalls.Load(), startupCalls.Load(), outputCalls.Load(), fingerprintCalls.Load(), resolverCalls.Load(), runner.calls.Load(), finalizer.calls.Load())
	}

	close(releaseFirst)
	var first runOutcome
	select {
	case first = <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first Run did not complete after startup release")
	}
	if first.err != nil || first.result.State != RunComplete {
		t.Fatalf("first Run result=%+v error=%v", first.result, first.err)
	}
	third, err := stage.Run(context.Background(), validRunRequest())
	if err != nil || third.State != RunComplete {
		t.Fatalf("non-overlapping Run after gate release result=%+v error=%v", third, err)
	}
	if idCalls.Load() != 2 || startupCalls.Load() != 2 || outputCalls.Load() != 2 || fingerprintCalls.Load() != 2 ||
		resolverCalls.Load() != 2 || runner.calls.Load() != 2 || finalizer.calls.Load() != 2 {
		t.Fatalf("accepted Run seam counts: id=%d startup=%d output=%d fingerprint=%d resolver=%d provider=%d finalizer=%d",
			idCalls.Load(), startupCalls.Load(), outputCalls.Load(), fingerprintCalls.Load(), resolverCalls.Load(), runner.calls.Load(), finalizer.calls.Load())
	}
}

func TestStartupSequenceOrdering(t *testing.T) {
	t.Parallel()
	config := testConfig()
	order := make([]string, 0, 6)
	config.StartupPrerequisite = func(context.Context, CollectionContext) error {
		order = append(order, "startup")
		return nil
	}
	config.OutputPathPrerequisite = func(string) error {
		order = append(order, "output")
		return nil
	}
	config.FingerprintProbe = func(context.Context, string) (fingerprint.TargetFingerprint, error) {
		order = append(order, "fingerprint")
		return testFingerprint(), nil
	}
	baseResolve := config.Resolve
	config.Resolve = func(request capability.Capability, target fingerprint.TargetFingerprint, candidates []provider.Descriptor, policy resolver.Policy) (resolver.Decision, error) {
		order = append(order, "resolver")
		return baseResolve(request, target, candidates, policy)
	}
	config.Providers[0].(*fakeRunner).execute = func(context.Context, capability.Capability, fingerprint.TargetFingerprint) execution.Result {
		order = append(order, "provider")
		return collectedResult()
	}
	config.Finalizer.(*fakeFinalizer).finalize = func(_ context.Context, _ CollectionContext, _ fingerprint.TargetFingerprint, records []CapabilityRecord) (FinalizationResult, error) {
		order = append(order, "finalizer")
		return successfulFinalization(records), nil
	}
	stage := mustFirstStage(t, config)
	if _, err := stage.Run(context.Background(), validRunRequest()); err != nil {
		t.Fatal(err)
	}
	want := []string{"startup", "output", "fingerprint", "resolver", "provider", "finalizer"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("startup sequence = %v, want %v", order, want)
	}
}

func TestPrerequisiteFailuresAreRunOwnedAndDoNotFinalize(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		mutate func(*FirstStageConfig, *atomic.Int32)
		cancel bool
		reason OrchestrationReason
	}{
		"caller cancelled": {cancel: true, reason: OrchestrationCancelled},
		"startup failed": {
			reason: OrchestrationStartupPrerequisiteFailed,
			mutate: func(config *FirstStageConfig, _ *atomic.Int32) {
				config.StartupPrerequisite = func(context.Context, CollectionContext) error { return errors.New("startup failed") }
			},
		},
		"output failed": {
			reason: OrchestrationStartupPrerequisiteFailed,
			mutate: func(config *FirstStageConfig, _ *atomic.Int32) {
				config.OutputPathPrerequisite = func(string) error { return errors.New("unsafe output") }
			},
		},
		"probe failed": {
			reason: OrchestrationStartupPrerequisiteFailed,
			mutate: func(config *FirstStageConfig, probes *atomic.Int32) {
				config.FingerprintProbe = func(context.Context, string) (fingerprint.TargetFingerprint, error) {
					probes.Add(1)
					return fingerprint.TargetFingerprint{}, errors.New("probe failed")
				}
			},
		},
		"fingerprint invalid": {
			reason: OrchestrationStartupPrerequisiteFailed,
			mutate: func(config *FirstStageConfig, probes *atomic.Int32) {
				config.FingerprintProbe = func(context.Context, string) (fingerprint.TargetFingerprint, error) {
					probes.Add(1)
					return fingerprint.TargetFingerprint{Platform: "windows"}, nil
				}
			},
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := testConfig()
			var probes atomic.Int32
			if test.mutate != nil {
				test.mutate(&config, &probes)
			}
			finalizer := config.Finalizer.(*fakeFinalizer)
			runner := config.Providers[0].(*fakeRunner)
			stage, err := NewFirstStage(config)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if test.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			result, err := stage.Run(ctx, validRunRequest())
			if err != nil {
				t.Fatalf("Run() structural error: %v", err)
			}
			if result.State != RunFailed || result.OrchestrationReason != test.reason {
				t.Fatalf("run result = %+v", result)
			}
			if finalizer.calls.Load() != 0 || runner.calls.Load() != 0 {
				t.Fatalf("prerequisite failure invoked finalizer/provider: %d/%d", finalizer.calls.Load(), runner.calls.Load())
			}
			for _, record := range result.Records {
				if record.Attempted || record.Execution.State != execution.Skipped || record.Execution.Reason != execution.ReasonNone || len(record.MissingEvidence) == 0 {
					t.Fatalf("invalid prerequisite accounting: %+v", record)
				}
			}
			if err := result.Validate(); err != nil {
				t.Fatalf("terminal failure result is invalid: %v", err)
			}
		})
	}
}

func TestCancellationDuringOutputPrerequisiteIsRunOwned(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	config := testConfig()
	config.OutputPathPrerequisite = func(string) error {
		cancel()
		return errors.New("output validation interrupted")
	}
	finalizer := config.Finalizer.(*fakeFinalizer)
	stage := mustFirstStage(t, config)
	result, err := stage.Run(ctx, validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationCancelled {
		t.Fatalf("output cancellation result = %+v", result)
	}
	if finalizer.calls.Load() != 0 || config.Providers[0].(*fakeRunner).calls.Load() != 0 {
		t.Fatal("output-prerequisite cancellation invoked finalizer or Provider")
	}
}

func TestCancellationDuringFingerprintPrerequisiteIsRunOwned(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	config := testConfig()
	config.FingerprintProbe = func(context.Context, string) (fingerprint.TargetFingerprint, error) {
		cancel()
		return testFingerprint(), nil
	}
	finalizer := config.Finalizer.(*fakeFinalizer)
	runner := config.Providers[0].(*fakeRunner)
	stage := mustFirstStage(t, config)
	result, err := stage.Run(ctx, validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationCancelled || result.Fingerprint != nil {
		t.Fatalf("fingerprint cancellation result = %+v", result)
	}
	if finalizer.calls.Load() != 0 || runner.calls.Load() != 0 {
		t.Fatal("fingerprint-prerequisite cancellation invoked finalizer or Provider")
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("fingerprint cancellation result is invalid: %v", err)
	}
}

func TestOrchestrationReasonLegalDomains(t *testing.T) {
	t.Parallel()
	unknown := CapabilityRecord{
		Request:             capability.CapabilityRequest{ID: "SYNTHETIC_UNKNOWN", Priority: capability.PriorityNormal},
		Compatibility:       execution.Unavailable,
		Execution:           unexecutedResult(execution.ReasonNone),
		OrchestrationReason: OrchestrationUnknownCapability,
		MissingEvidence:     []string{"Unknown capability."},
	}
	if err := unknown.Validate(); err != nil {
		t.Fatalf("unknown accounting rejected: %v", err)
	}
	for _, reason := range []OrchestrationReason{OrchestrationStartupPrerequisiteFailed, OrchestrationPackageFinalizationFailed} {
		record := unknown
		record.OrchestrationReason = reason
		if err := record.Validate(); err == nil {
			t.Fatalf("capability record accepted run reason %s", reason)
		}
	}

	baseRun := RunResult{
		Context: CollectionContext{
			CollectionID: testCollectionID, OutputDestination: `C:\output`, StartedAt: testNow().Format(time.RFC3339Nano),
		},
		State: RunFailed, Records: []CapabilityRecord{unknown},
	}
	for _, reason := range []OrchestrationReason{OrchestrationStartupPrerequisiteFailed, OrchestrationCancelled, OrchestrationPackageFinalizationFailed} {
		run := baseRun
		run.OrchestrationReason = reason
		if reason == OrchestrationPackageFinalizationFailed {
			target := testFingerprint()
			run.Fingerprint = &target
		}
		if err := run.Validate(); err != nil {
			t.Fatalf("run reason %s rejected: %v", reason, err)
		}
	}
	for _, reason := range []OrchestrationReason{OrchestrationUnknownCapability, OrchestrationResolutionFailed} {
		run := baseRun
		run.OrchestrationReason = reason
		if err := run.Validate(); err == nil {
			t.Fatalf("run accepted capability reason %s", reason)
		}
	}
}

func TestCapabilityRecordValidatesProviderCapabilitySupport(t *testing.T) {
	t.Parallel()
	definition := testCapability("SYNTHETIC_A", capability.StateSnapshot)
	descriptor := newFakeRunner("synthetic-provider", []string{"SYNTHETIC_B"}, collectedResult()).descriptor
	decision := resolver.Decision{Selected: &descriptor, Compatibility: execution.Available, Reason: execution.ReasonNone}
	record := CapabilityRecord{
		Request:          capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityNormal},
		Capability:       &definition,
		Compatibility:    execution.Available,
		Decision:         &decision,
		SelectedProvider: &descriptor,
		Attempted:        true,
		Execution:        collectedResult(),
	}
	if err := record.Validate(); err == nil {
		t.Fatal("record accepted a selected Provider that does not support the requested Capability")
	}

	descriptor.Capabilities = []string{definition.ID}
	decision.Selected = &descriptor
	record.SelectedProvider = &descriptor
	if err := record.Validate(); err != nil {
		t.Fatalf("record rejected a Provider supporting the requested Capability: %v", err)
	}

	foreignDecisionDescriptor := descriptor
	foreignDecisionDescriptor.Capabilities = []string{"SYNTHETIC_B"}
	decision.Selected = &foreignDecisionDescriptor
	if err := record.Validate(); err == nil {
		t.Fatal("record accepted a Resolver-selected descriptor that does not support the requested Capability")
	}
}

func TestSelectedNonAttemptedAccountingRequiresCancellation(t *testing.T) {
	t.Parallel()
	definition := testCapability("SYNTHETIC_A", capability.StateSnapshot)
	descriptor := newFakeRunner("synthetic-provider", []string{definition.ID}, collectedResult()).descriptor
	decision := resolver.Decision{Selected: &descriptor, Compatibility: execution.Available, Reason: execution.ReasonNone}
	record := CapabilityRecord{
		Request:          capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityNormal},
		Capability:       &definition,
		Compatibility:    execution.Available,
		Decision:         &decision,
		SelectedProvider: &descriptor,
		Execution:        unexecutedResult(execution.ReasonNone),
		MissingEvidence:  []string{"Selected Provider was not executed."},
	}
	if err := record.Validate(); err == nil {
		t.Fatal("selected non-attempted accounting without CANCELLED reason unexpectedly validated")
	}
	record.OrchestrationReason = OrchestrationCancelled
	if err := record.Validate(); err != nil {
		t.Fatalf("selected non-attempted CANCELLED accounting was rejected: %v", err)
	}

	record.Attempted = true
	record.OrchestrationReason = ""
	record.Execution = execution.Result{
		State: execution.Skipped, Reason: execution.ReasonPolicyDisabled,
		SideEffectSummary: "Provider executed and returned SKIPPED.",
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("actual Provider SKIPPED accounting was rejected: %v", err)
	}
}

func TestUnknownAndResolutionFailureAccounting(t *testing.T) {
	t.Parallel()
	t.Run("unknown bypasses resolver and provider", func(t *testing.T) {
		config := testConfig()
		var resolverCalls atomic.Int32
		baseResolve := config.Resolve
		config.Resolve = func(request capability.Capability, target fingerprint.TargetFingerprint, candidates []provider.Descriptor, policy resolver.Policy) (resolver.Decision, error) {
			resolverCalls.Add(1)
			return baseResolve(request, target, candidates, policy)
		}
		stage := mustFirstStage(t, config)
		request := validRunRequest()
		request.Supplemental = []capability.CapabilityRequest{{ID: "SYNTHETIC_UNKNOWN", Priority: capability.PriorityNormal}}
		result, err := stage.Run(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		unknown := recordByID(t, result, "SYNTHETIC_UNKNOWN")
		if unknown.Attempted || unknown.Capability != nil || unknown.Decision != nil || unknown.SelectedProvider != nil ||
			unknown.Compatibility != execution.Unavailable || unknown.Execution.State != execution.Skipped ||
			unknown.Execution.Reason != execution.ReasonNone || unknown.OrchestrationReason != OrchestrationUnknownCapability ||
			len(unknown.MissingEvidence) == 0 {
			t.Fatalf("invalid unknown accounting: %+v", unknown)
		}
		if resolverCalls.Load() != 1 || config.Providers[0].(*fakeRunner).calls.Load() != 1 {
			t.Fatalf("unknown request affected invocation counts: resolver=%d provider=%d", resolverCalls.Load(), config.Providers[0].(*fakeRunner).calls.Load())
		}
	})

	t.Run("resolver failure is not provider failure", func(t *testing.T) {
		config := testConfig()
		config.Resolve = func(capability.Capability, fingerprint.TargetFingerprint, []provider.Descriptor, resolver.Policy) (resolver.Decision, error) {
			return resolver.Decision{}, errors.New("resolver seam failed")
		}
		stage := mustFirstStage(t, config)
		result, err := stage.Run(context.Background(), validRunRequest())
		if err != nil {
			t.Fatal(err)
		}
		record := result.Records[0]
		if record.Attempted || record.OrchestrationReason != OrchestrationResolutionFailed || record.Execution.Reason != execution.ReasonNone || record.SelectedProvider != nil {
			t.Fatalf("invalid resolution-failure accounting: %+v", record)
		}
		if config.Providers[0].(*fakeRunner).calls.Load() != 0 {
			t.Fatal("resolver failure executed a provider")
		}
		if result.State != RunPartial {
			t.Fatalf("result state = %s, want PARTIAL", result.State)
		}
	})
}

func TestSelectedNonAttemptedPackagePreparationFailureAccounting(t *testing.T) {
	t.Parallel()
	config := testConfig()
	definition := config.CapabilityCatalog[0]
	descriptor := config.Providers[0].Descriptor()
	decision := resolver.Decision{
		Selected:      &descriptor,
		Compatibility: execution.Available,
		Reason:        execution.ReasonNone,
		Evaluations: []resolver.CandidateEvaluation{{
			ProviderID: descriptor.ID, Compatibility: execution.Available, Reason: execution.ReasonNone, Eligible: true,
		}},
	}
	record := CapabilityRecord{
		Request:             config.ProtectedBaseline[0],
		Capability:          &definition,
		Compatibility:       execution.Available,
		Decision:            &decision,
		SelectedProvider:    &descriptor,
		Attempted:           false,
		Execution:           unexecutedResult(execution.ReasonNone),
		OrchestrationReason: OrchestrationPackageFinalizationFailed,
		MissingEvidence:     []string{"The requested evidence is absent because package staging preparation failed before Provider execution."},
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("exact pre-attempt package failure was rejected: %v", err)
	}
	for name, mutate := range map[string]func(*CapabilityRecord){
		"empty evidence":     func(value *CapabilityRecord) { value.MissingEvidence = nil },
		"artifact reference": func(value *CapabilityRecord) { value.ArtifactReference = "derived/process-identity-snapshot.ndjson" },
		"wrong reason":       func(value *CapabilityRecord) { value.OrchestrationReason = OrchestrationResolutionFailed },
		"attempted":          func(value *CapabilityRecord) { value.Attempted = true },
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			candidate := record
			candidate.MissingEvidence = append([]string(nil), record.MissingEvidence...)
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatalf("invalid package-preparation record unexpectedly validated: %+v", candidate)
			}
		})
	}

	cancelled := record
	cancelled.OrchestrationReason = OrchestrationCancelled
	cancelled.MissingEvidence = []string{"The request was not executed because the run was cancelled."}
	if err := cancelled.Validate(); err != nil {
		t.Fatalf("existing selected cancellation contract changed: %v", err)
	}
}

func TestResolverCannotSubstituteAnUntrustedProviderDescriptor(t *testing.T) {
	t.Parallel()
	config := testConfig()
	config.Resolve = func(_ capability.Capability, _ fingerprint.TargetFingerprint, candidates []provider.Descriptor, _ resolver.Policy) (resolver.Decision, error) {
		selected := candidates[0]
		selected.Class = provider.ExternalBackend
		return resolver.Decision{Selected: &selected, Compatibility: execution.Available, Reason: execution.ReasonNone}, nil
	}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(context.Background(), validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if record.Attempted || record.SelectedProvider != nil || record.OrchestrationReason != OrchestrationResolutionFailed {
		t.Fatalf("untrusted descriptor selection was not rejected: %+v", record)
	}
	if config.Providers[0].(*fakeRunner).calls.Load() != 0 {
		t.Fatal("untrusted descriptor selection executed a Provider")
	}
}

func TestResolverCannotSelectTrustedProviderForForeignCapability(t *testing.T) {
	t.Parallel()
	config := testConfig()
	runner := config.Providers[0].(*fakeRunner)
	runner.descriptor.Capabilities = []string{"SYNTHETIC_OTHER"}
	config.Resolve = func(_ capability.Capability, _ fingerprint.TargetFingerprint, candidates []provider.Descriptor, _ resolver.Policy) (resolver.Decision, error) {
		selected := candidates[0]
		return resolver.Decision{Selected: &selected, Compatibility: execution.Available, Reason: execution.ReasonNone}, nil
	}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(context.Background(), validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if record.Attempted || record.SelectedProvider != nil || record.Decision != nil ||
		record.Compatibility != execution.Unavailable || record.Execution.State != execution.Skipped ||
		record.Execution.Reason != execution.ReasonNone || record.OrchestrationReason != OrchestrationResolutionFailed ||
		len(record.MissingEvidence) == 0 {
		t.Fatalf("foreign-capability trusted descriptor was not rejected honestly: %+v", record)
	}
	if runner.calls.Load() != 0 {
		t.Fatal("foreign-capability trusted descriptor executed a Provider")
	}
}

func TestFingerprintIsProbedOnceAndClonedAcrossBoundaries(t *testing.T) {
	t.Parallel()
	config := testConfig()
	var probes atomic.Int32
	config.FingerprintProbe = func(context.Context, string) (fingerprint.TargetFingerprint, error) {
		probes.Add(1)
		return testFingerprint(), nil
	}
	baseResolve := config.Resolve
	config.Resolve = func(request capability.Capability, target fingerprint.TargetFingerprint, candidates []provider.Descriptor, policy resolver.Policy) (resolver.Decision, error) {
		decision, err := baseResolve(request, target, candidates, policy)
		*target.Probe.OSVersion.Value = "resolver-mutated"
		return decision, err
	}
	runner := config.Providers[0].(*fakeRunner)
	runner.execute = func(_ context.Context, _ capability.Capability, target fingerprint.TargetFingerprint) execution.Result {
		*target.Probe.OSVersion.Value = "provider-mutated"
		return collectedResult()
	}
	finalizer := config.Finalizer.(*fakeFinalizer)
	finalizer.finalize = func(_ context.Context, _ CollectionContext, target fingerprint.TargetFingerprint, records []CapabilityRecord) (FinalizationResult, error) {
		*target.Probe.OSVersion.Value = "finalizer-mutated"
		records[0].Request.ID = "MUTATED"
		records[0].Execution.State = execution.Failed
		return successfulFinalization(recordsWithOriginalID(records, "SYNTHETIC_BASELINE")), nil
	}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(context.Background(), validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 {
		t.Fatalf("fingerprint probe calls = %d, want 1", probes.Load())
	}
	if got := *result.Fingerprint.Probe.OSVersion.Value; got != "10.0" {
		t.Fatalf("retained fingerprint was mutated: %q", got)
	}
	if result.Records[0].Request.ID != "SYNTHETIC_BASELINE" || result.Records[0].Execution.State != execution.Collected {
		t.Fatalf("finalizer mutated retained records: %+v", result.Records[0])
	}
	if result.State != RunComplete {
		t.Fatalf("result state = %s, want COMPLETE", result.State)
	}
}

func TestUnavailableAndProviderSkippedRemainDistinct(t *testing.T) {
	t.Parallel()
	t.Run("resolver unavailable", func(t *testing.T) {
		config := testConfig()
		config.Providers = nil
		stage := mustFirstStage(t, config)
		result, err := stage.Run(context.Background(), validRunRequest())
		if err != nil {
			t.Fatal(err)
		}
		record := result.Records[0]
		if record.Attempted || record.OrchestrationReason != "" || record.Decision == nil ||
			record.Compatibility != execution.Unavailable || record.Execution.Reason != execution.ReasonDependencyMissing {
			t.Fatalf("invalid unavailable accounting: %+v", record)
		}
	})

	t.Run("provider returned skipped", func(t *testing.T) {
		config := testConfig()
		runner := config.Providers[0].(*fakeRunner)
		runner.result = execution.Result{
			State: execution.Skipped, Reason: execution.ReasonPolicyDisabled,
			SideEffectSummary: "Provider executed and skipped by policy.",
		}
		stage := mustFirstStage(t, config)
		result, err := stage.Run(context.Background(), validRunRequest())
		if err != nil {
			t.Fatal(err)
		}
		record := result.Records[0]
		if !record.Attempted || record.Execution.State != execution.Skipped || record.OrchestrationReason != "" {
			t.Fatalf("actual Provider SKIPPED lost attempted provenance: %+v", record)
		}
	})
}

func TestCancellationBeforeLaunchRetainsSelectionAndUsesIndependentFinalizerContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	config := testConfig()
	baseResolve := config.Resolve
	config.Resolve = func(request capability.Capability, target fingerprint.TargetFingerprint, candidates []provider.Descriptor, policy resolver.Policy) (resolver.Decision, error) {
		decision, err := baseResolve(request, target, candidates, policy)
		cancel()
		return decision, err
	}
	finalizer := config.Finalizer.(*fakeFinalizer)
	finalizer.finalize = func(ctx context.Context, _ CollectionContext, _ fingerprint.TargetFingerprint, records []CapabilityRecord) (FinalizationResult, error) {
		if ctx.Err() != nil {
			return FinalizationResult{}, fmt.Errorf("finalizer inherited cancelled context: %w", ctx.Err())
		}
		return successfulFinalization(records), nil
	}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(ctx, validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if record.SelectedProvider == nil || record.Attempted || record.OrchestrationReason != OrchestrationCancelled || record.Execution.Reason != execution.ReasonNone {
		t.Fatalf("cancel-before-launch accounting is invalid: %+v", record)
	}
	if config.Providers[0].(*fakeRunner).calls.Load() != 0 {
		t.Fatal("cancel-before-launch invoked Provider")
	}
	if result.State != RunPartial || result.OrchestrationReason != OrchestrationCancelled || !result.FinalizationVerified {
		t.Fatalf("cancelled run result = %+v", result)
	}
}

func TestSequentialExecutionNoFallbackAndPriorRecordsSurvive(t *testing.T) {
	t.Parallel()
	firstCapability := testCapability("SYNTHETIC_FIRST", capability.StateSnapshot)
	secondCapability := testCapability("SYNTHETIC_SECOND", capability.StateSnapshot)
	first := newFakeRunner("synthetic-first", []string{firstCapability.ID, secondCapability.ID}, failedResult())
	second := newFakeRunner("synthetic-fallback", []string{firstCapability.ID, secondCapability.ID}, collectedResult())
	first.execute = func(_ context.Context, request capability.Capability, _ fingerprint.TargetFingerprint) execution.Result {
		if request.ID == firstCapability.ID {
			return failedResult()
		}
		return collectedResult()
	}
	config := testConfig()
	config.CapabilityCatalog = []capability.Capability{firstCapability, secondCapability}
	config.ProtectedBaseline = []capability.CapabilityRequest{
		{ID: firstCapability.ID, Priority: capability.PriorityEarly, Protected: true},
		{ID: secondCapability.ID, Priority: capability.PriorityNormal, Protected: true},
	}
	config.Providers = []provider.Runner{first, second}
	config.Resolve = func(request capability.Capability, _ fingerprint.TargetFingerprint, candidates []provider.Descriptor, _ resolver.Policy) (resolver.Decision, error) {
		selected := candidates[0]
		return resolver.Decision{Selected: &selected, Compatibility: execution.Available, Reason: execution.ReasonNone}, nil
	}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(context.Background(), validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if first.calls.Load() != 2 || first.maximum.Load() != 1 || second.calls.Load() != 0 {
		t.Fatalf("execution calls/max/fallback = %d/%d/%d", first.calls.Load(), first.maximum.Load(), second.calls.Load())
	}
	if result.Records[0].Execution.State != execution.Failed || result.Records[1].Execution.State != execution.Collected {
		t.Fatalf("prior/later records not preserved: %+v", result.Records)
	}
	if result.State != RunPartial {
		t.Fatalf("run state = %s, want PARTIAL", result.State)
	}
}

func TestProviderCancellationAccountsRemainingRequests(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	firstCapability := testCapability("SYNTHETIC_FIRST", capability.StateSnapshot)
	secondCapability := testCapability("SYNTHETIC_SECOND", capability.StateSnapshot)
	runner := newFakeRunner("synthetic-provider", []string{firstCapability.ID, secondCapability.ID}, collectedResult())
	runner.execute = func(_ context.Context, request capability.Capability, _ fingerprint.TargetFingerprint) execution.Result {
		if request.ID == firstCapability.ID {
			cancel()
			return execution.Result{State: execution.Failed, Reason: execution.ReasonTimeout, SideEffectSummary: "Provider observed cancellation."}
		}
		return collectedResult()
	}
	config := testConfig()
	config.CapabilityCatalog = []capability.Capability{firstCapability, secondCapability}
	config.ProtectedBaseline = []capability.CapabilityRequest{
		{ID: firstCapability.ID, Priority: capability.PriorityEarly, Protected: true},
		{ID: secondCapability.ID, Priority: capability.PriorityNormal, Protected: true},
	}
	config.Providers = []provider.Runner{runner}
	finalizer := config.Finalizer.(*fakeFinalizer)
	finalizer.finalize = func(ctx context.Context, _ CollectionContext, _ fingerprint.TargetFingerprint, records []CapabilityRecord) (FinalizationResult, error) {
		if ctx.Err() != nil {
			return FinalizationResult{}, ctx.Err()
		}
		return successfulFinalization(records), nil
	}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(ctx, validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls.Load() != 1 || runner.maximum.Load() != 1 {
		t.Fatalf("provider calls/max = %d/%d", runner.calls.Load(), runner.maximum.Load())
	}
	if !result.Records[0].Attempted || result.Records[0].Execution.State != execution.Failed {
		t.Fatalf("active Provider result was not preserved: %+v", result.Records[0])
	}
	if result.Records[1].Attempted || result.Records[1].OrchestrationReason != OrchestrationCancelled {
		t.Fatalf("remaining request was not cancelled: %+v", result.Records[1])
	}
	if result.State != RunPartial || result.OrchestrationReason != OrchestrationCancelled {
		t.Fatalf("cancelled run result = %+v", result)
	}
}

func TestFinalizerFailureAndReferenceValidationForceRunFailed(t *testing.T) {
	t.Parallel()
	tests := map[string]func([]CapabilityRecord) (FinalizationResult, error){
		"error": func([]CapabilityRecord) (FinalizationResult, error) {
			return FinalizationResult{}, errors.New("finalizer failed")
		},
		"unverified": func(records []CapabilityRecord) (FinalizationResult, error) {
			result := successfulFinalization(records)
			result.Verified = false
			return result, nil
		},
		"blank package": func(records []CapabilityRecord) (FinalizationResult, error) {
			result := successfulFinalization(records)
			result.PackageReference = " "
			return result, nil
		},
		"missing receipt": func(records []CapabilityRecord) (FinalizationResult, error) {
			result := successfulFinalization(records)
			result.References[0].ReceiptReference = ""
			return result, nil
		},
		"duplicate reference": func(records []CapabilityRecord) (FinalizationResult, error) {
			result := successfulFinalization(records)
			result.References = append(result.References, result.References[0])
			return result, nil
		},
		"unknown reference": func(records []CapabilityRecord) (FinalizationResult, error) {
			result := successfulFinalization(records)
			result.References[0].CapabilityID = "SYNTHETIC_UNKNOWN"
			return result, nil
		},
	}
	for name, finalize := range tests {
		name, finalize := name, finalize
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := testConfig()
			config.Finalizer = &fakeFinalizer{finalize: func(_ context.Context, _ CollectionContext, _ fingerprint.TargetFingerprint, records []CapabilityRecord) (FinalizationResult, error) {
				return finalize(records)
			}}
			stage := mustFirstStage(t, config)
			result, err := stage.Run(context.Background(), validRunRequest())
			if err != nil {
				t.Fatal(err)
			}
			if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || result.FinalizationVerified {
				t.Fatalf("invalid finalization failure result: %+v", result)
			}
			if result.Records[0].Execution.State != execution.Collected {
				t.Fatal("finalization failure overwrote completed execution facts")
			}
		})
	}
}

func TestFinalizationRejectsDuplicateReceiptReferences(t *testing.T) {
	t.Parallel()
	records := []CapabilityRecord{
		{Request: capability.CapabilityRequest{ID: "SYNTHETIC_FIRST", Priority: capability.PriorityEarly}},
		{Request: capability.CapabilityRequest{ID: "SYNTHETIC_SECOND", Priority: capability.PriorityNormal}},
	}
	result := FinalizationResult{
		Verified: true, PackageReference: "package:synthetic",
		References: []FinalizationReference{
			{CapabilityID: "SYNTHETIC_FIRST", ReceiptReference: "receipt:duplicate"},
			{CapabilityID: "SYNTHETIC_SECOND", ReceiptReference: "receipt:duplicate"},
		},
	}
	if err := validateFinalization(result, records); err == nil {
		t.Fatal("duplicate receipt references unexpectedly validated")
	}
}

func TestInvalidProviderResultUsesExistingProviderErrorReason(t *testing.T) {
	t.Parallel()
	config := testConfig()
	runner := config.Providers[0].(*fakeRunner)
	runner.result = execution.Result{State: "INVALID", Reason: "INVALID"}
	stage := mustFirstStage(t, config)
	result, err := stage.Run(context.Background(), validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if !record.Attempted || record.Execution.State != execution.Failed || record.Execution.Reason != execution.ReasonProviderError {
		t.Fatalf("invalid Provider result was not normalized with existing authority: %+v", record)
	}
	if result.State != RunPartial {
		t.Fatalf("run state = %s, want PARTIAL", result.State)
	}
}

func TestFinalizerTimeoutIsBounded(t *testing.T) {
	t.Parallel()
	config := testConfig()
	config.FinalizationTimeout = 20 * time.Millisecond
	config.Finalizer = &fakeFinalizer{finalize: func(ctx context.Context, _ CollectionContext, _ fingerprint.TargetFingerprint, _ []CapabilityRecord) (FinalizationResult, error) {
		<-ctx.Done()
		return FinalizationResult{}, ctx.Err()
	}}
	stage := mustFirstStage(t, config)
	started := time.Now()
	result, err := stage.Run(context.Background(), validRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded finalization took %s", elapsed)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed {
		t.Fatalf("timeout result = %+v", result)
	}
}

func TestRunStateDerivation(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		compatibility execution.CompatibilityState
		state         execution.State
		want          RunState
	}{
		"complete": {execution.Available, execution.Collected, RunComplete},
		"degraded": {execution.Degraded, execution.Collected, RunPartial},
		"partial":  {execution.Available, execution.Partial, RunPartial},
		"failed":   {execution.Available, execution.Failed, RunPartial},
		"blocked":  {execution.Degraded, execution.Blocked, RunPartial},
		"skipped":  {execution.Available, execution.Skipped, RunPartial},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := testConfig()
			runner := config.Providers[0].(*fakeRunner)
			runner.result = execution.Result{
				State: test.state, Reason: reasonForState(test.state), SideEffectSummary: "Synthetic Provider execution.",
			}
			config.Resolve = func(_ capability.Capability, _ fingerprint.TargetFingerprint, candidates []provider.Descriptor, _ resolver.Policy) (resolver.Decision, error) {
				selected := candidates[0]
				return resolver.Decision{Selected: &selected, Compatibility: test.compatibility, Reason: reasonForCompatibility(test.compatibility)}, nil
			}
			stage := mustFirstStage(t, config)
			result, err := stage.Run(context.Background(), validRunRequest())
			if err != nil {
				t.Fatal(err)
			}
			if result.State != test.want {
				t.Fatalf("run state = %s, want %s", result.State, test.want)
			}
			if test.want == RunComplete && len(result.Records[0].MissingEvidence) != 0 {
				t.Fatal("complete result contains missing evidence")
			}
			if test.want == RunPartial && len(result.Records[0].MissingEvidence) == 0 {
				t.Fatal("partial result contains no missing evidence")
			}
		})
	}
}

func testConfig() FirstStageConfig {
	definition := testCapability("SYNTHETIC_BASELINE", capability.StateSnapshot)
	runner := newFakeRunner("synthetic-provider", []string{definition.ID}, collectedResult())
	return FirstStageConfig{
		CapabilityCatalog: []capability.Capability{definition},
		ProtectedBaseline: []capability.CapabilityRequest{{ID: definition.ID, Priority: capability.PriorityNormal, Protected: true}},
		Providers:         []provider.Runner{runner},
		GenerateCollectionID: func() (string, error) {
			return testCollectionID, nil
		},
		Clock: func() time.Time { return testNow() },
		StartupPrerequisite: func(context.Context, CollectionContext) error {
			return nil
		},
		OutputPathPrerequisite: func(string) error { return nil },
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return testFingerprint(), nil
		},
		Resolve:             resolver.Resolve,
		Finalizer:           &fakeFinalizer{},
		FinalizationTimeout: time.Second,
	}
}

func newFakeRunner(id string, capabilityIDs []string, result execution.Result) *fakeRunner {
	return &fakeRunner{
		descriptor: provider.Descriptor{
			ID: id, Class: provider.SyntheticTest, Capabilities: append([]string(nil), capabilityIDs...),
			Requirements: provider.Requirements{Available: true, AvailabilityReason: execution.ReasonNone},
			Quality: provider.Quality{
				Compatibility: execution.Available, Reason: execution.ReasonNone,
				Fidelity: 1, Disturbance: 0, Completeness: 1, OutputStability: 1, EvidenceValue: 1,
			},
		},
		result: result,
	}
}

func testCapability(id string, semantics capability.AcquisitionSemantics) capability.Capability {
	return capability.Capability{ID: id, Description: "Synthetic orchestration test capability.", AcquisitionSemantics: semantics, Sensitivity: "synthetic"}
}

func validRunRequest() RunRequest {
	return RunRequest{CaseID: "CASE-01", OutputDestination: `C:\synthetic\output`}
}

func testNow() time.Time {
	return time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
}

func testFingerprint() fingerprint.TargetFingerprint {
	stringField := func(value string) fingerprint.Field[string] {
		copy := value
		return fingerprint.Field[string]{State: fingerprint.Known, Value: &copy, Source: "synthetic-test", CapturedAt: testNow().Format(time.RFC3339Nano)}
	}
	uint32Field := func(value uint32) fingerprint.Field[uint32] {
		copy := value
		return fingerprint.Field[uint32]{State: fingerprint.Known, Value: &copy, Source: "synthetic-test", CapturedAt: testNow().Format(time.RFC3339Nano)}
	}
	uint64Field := func(value uint64) fingerprint.Field[uint64] {
		copy := value
		return fingerprint.Field[uint64]{State: fingerprint.Known, Value: &copy, Source: "synthetic-test", CapturedAt: testNow().Format(time.RFC3339Nano)}
	}
	boolField := func(value bool) fingerprint.Field[bool] {
		copy := value
		return fingerprint.Field[bool]{State: fingerprint.Known, Value: &copy, Source: "synthetic-test", CapturedAt: testNow().Format(time.RFC3339Nano)}
	}
	return fingerprint.TargetFingerprint{
		Platform: "windows", OSFamily: "WindowsNT", Version: "10.0", Build: "26100", Architecture: "amd64", Privilege: "standard-user",
		Probe: &fingerprint.ProbeFields{
			OSVersion: stringField("10.0"), OSBuild: stringField("26100"), NativeArchitecture: stringField("amd64"), ProcessArchitecture: stringField("amd64"),
			LogicalProcessorCount: uint32Field(8), TotalPhysicalMemoryBytes: uint64Field(16 << 30), AvailablePhysicalMemoryBytes: uint64Field(8 << 30),
			Elevated: boolField(false), TokenElevationType: stringField("default"),
			OutputVolume: fingerprint.OutputVolumeFacts{
				ValidatedOutputPath: stringField(`C:\synthetic\output`), VolumeRoot: stringField(`C:\`), DriveType: stringField("FIXED"),
				FileSystem: stringField("NTFS"), AvailableBytesCaller: uint64Field(1 << 30),
			},
		},
	}
}

func collectedResult() execution.Result {
	return execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Synthetic Provider returned fixed test facts."}
}

func failedResult() execution.Result {
	return execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, SideEffectSummary: "Synthetic Provider returned a fixed failure."}
}

func successfulFinalization(records []CapabilityRecord) FinalizationResult {
	references := make([]FinalizationReference, 0, len(records))
	for _, record := range records {
		references = append(references, FinalizationReference{
			CapabilityID: record.Request.ID, ReceiptReference: "receipt:" + record.Request.ID,
		})
	}
	return FinalizationResult{Verified: true, PackageReference: "package:synthetic", References: references}
}

func recordsWithOriginalID(records []CapabilityRecord, id string) []CapabilityRecord {
	clone := cloneRecords(records)
	clone[0].Request.ID = id
	return clone
}

func mustFirstStage(t *testing.T, config FirstStageConfig) *FirstStage {
	t.Helper()
	stage, err := NewFirstStage(config)
	if err != nil {
		t.Fatalf("NewFirstStage() error: %v", err)
	}
	return stage
}

func recordByID(t *testing.T, result RunResult, id string) CapabilityRecord {
	t.Helper()
	for _, record := range result.Records {
		if record.Request.ID == id {
			return record
		}
	}
	t.Fatalf("record %s not found", id)
	return CapabilityRecord{}
}

func reasonForState(state execution.State) execution.Reason {
	switch state {
	case execution.Collected:
		return execution.ReasonNone
	case execution.Partial:
		return execution.ReasonAPIUnavailable
	case execution.Skipped:
		return execution.ReasonPolicyDisabled
	case execution.Failed:
		return execution.ReasonProviderError
	case execution.Blocked:
		return execution.ReasonPrivilegeRequired
	default:
		return execution.ReasonProviderError
	}
}

func reasonForCompatibility(state execution.CompatibilityState) execution.Reason {
	if state == execution.Available {
		return execution.ReasonNone
	}
	return execution.ReasonPrivilegeRequired
}
