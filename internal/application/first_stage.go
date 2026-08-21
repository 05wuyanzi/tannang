// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package application coordinates synthetic collection and the library-only
// first-stage orchestration contract.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/receipt"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

// OrchestrationReason describes application-owned accounting and run outcomes.
// It never replaces a resolver or provider execution reason.
type OrchestrationReason string

const (
	OrchestrationUnknownCapability         OrchestrationReason = "UNKNOWN_CAPABILITY"
	OrchestrationResolutionFailed          OrchestrationReason = "RESOLUTION_FAILED"
	OrchestrationStartupPrerequisiteFailed OrchestrationReason = "STARTUP_PREREQUISITE_FAILED"
	OrchestrationCancelled                 OrchestrationReason = "CANCELLED"
	OrchestrationPackageFinalizationFailed OrchestrationReason = "PACKAGE_FINALIZATION_FAILED"
)

// RunState is independent from the state of any single provider execution.
type RunState string

const (
	RunComplete RunState = "COMPLETE"
	RunPartial  RunState = "PARTIAL"
	RunFailed   RunState = "FAILED"
)

// RunRequest contains only operator-owned run input. Protected baseline
// membership is intentionally absent.
type RunRequest struct {
	CaseID            string                         `json:"case_id,omitempty"`
	OutputDestination string                         `json:"output_destination"`
	Supplemental      []capability.CapabilityRequest `json:"supplemental,omitempty"`
}

// CollectionContext is immutable after construction.
type CollectionContext struct {
	CollectionID      string `json:"collection_id"`
	CaseID            string `json:"case_id,omitempty"`
	OutputDestination string `json:"output_destination"`
	StartedAt         string `json:"started_at"`
}

// CapabilityRecord accounts for exactly one accepted request. Attempted keeps
// application-created SKIPPED accounting distinct from a provider that ran and
// returned SKIPPED.
type CapabilityRecord struct {
	Request             capability.CapabilityRequest `json:"request"`
	Capability          *capability.Capability       `json:"capability,omitempty"`
	Compatibility       execution.CompatibilityState `json:"compatibility"`
	Decision            *resolver.Decision           `json:"decision,omitempty"`
	SelectedProvider    *provider.Descriptor         `json:"selected_provider,omitempty"`
	Attempted           bool                         `json:"attempted"`
	Execution           execution.Result             `json:"execution"`
	OrchestrationReason OrchestrationReason          `json:"orchestration_reason,omitempty"`
	MissingEvidence     []string                     `json:"missing_evidence,omitempty"`
	ReceiptReference    string                       `json:"receipt_reference,omitempty"`
	ArtifactReference   string                       `json:"artifact_reference,omitempty"`
}

// RunResult is the application-owned terminal accounting result.
type RunResult struct {
	Context              CollectionContext              `json:"context"`
	State                RunState                       `json:"state"`
	OrchestrationReason  OrchestrationReason            `json:"orchestration_reason,omitempty"`
	Detail               string                         `json:"detail,omitempty"`
	Fingerprint          *fingerprint.TargetFingerprint `json:"fingerprint,omitempty"`
	Records              []CapabilityRecord             `json:"records"`
	FinalizationVerified bool                           `json:"finalization_verified"`
	PackageReference     string                         `json:"package_reference,omitempty"`
}

// FinalizationReference associates an opaque receipt reference and optional
// artifact reference with one accepted request. It carries no artifact data or
// transport semantics.
type FinalizationReference struct {
	CapabilityID      string `json:"capability_id"`
	ReceiptReference  string `json:"receipt_reference"`
	ArtifactReference string `json:"artifact_reference,omitempty"`
}

// FinalizationResult contains only orchestration-visible verification and
// opaque references.
type FinalizationResult struct {
	Verified         bool                    `json:"verified"`
	PackageReference string                  `json:"package_reference"`
	References       []FinalizationReference `json:"references"`
}

// Finalizer is a reference/outcome-only seam. This slice supplies only fakes
// in tests and does not implement multi-capability package materialization.
type Finalizer interface {
	Finalize(context.Context, CollectionContext, fingerprint.TargetFingerprint, []CapabilityRecord) (FinalizationResult, error)
}

// ResolverFunc matches the committed resolver authority while allowing pure
// deterministic orchestration tests.
type ResolverFunc func(
	capability.Capability,
	fingerprint.TargetFingerprint,
	[]provider.Descriptor,
	resolver.Policy,
) (resolver.Decision, error)

// FirstStageConfig is trusted configuration copied and validated by
// NewFirstStage. No seam receives a silent success default.
type FirstStageConfig struct {
	CapabilityCatalog      []capability.Capability
	ProtectedBaseline      []capability.CapabilityRequest
	Providers              []provider.Runner
	ResolverPolicy         resolver.Policy
	GenerateCollectionID   func() (string, error)
	Clock                  func() time.Time
	StartupPrerequisite    func(context.Context, CollectionContext) error
	OutputPathPrerequisite func(string) error
	FingerprintProbe       func(context.Context, string) (fingerprint.TargetFingerprint, error)
	Resolve                ResolverFunc
	Finalizer              Finalizer
	FinalizationTimeout    time.Duration
}

// FirstStage coordinates one Run at a time. Overlapping Run calls on the same
// instance are rejected; Provider execution within an accepted Run is
// synchronous and sequential.
type FirstStage struct {
	runMu                  sync.Mutex
	runActive              bool
	catalog                map[string]capability.Capability
	baseline               []capability.CapabilityRequest
	providers              map[string]provider.Runner
	providerDescriptors    map[string]provider.Descriptor
	descriptors            []provider.Descriptor
	resolverPolicy         resolver.Policy
	generateCollectionID   func() (string, error)
	clock                  func() time.Time
	startupPrerequisite    func(context.Context, CollectionContext) error
	outputPathPrerequisite func(string) error
	fingerprintProbe       func(context.Context, string) (fingerprint.TargetFingerprint, error)
	resolve                ResolverFunc
	finalizer              Finalizer
	finalizationTimeout    time.Duration
	realMode               bool
	streamingRunner        provider.StreamingRunner
	streamingDescriptor    provider.Descriptor
	packageFactory         firstStagePackageFactory
	runtimeSink            RuntimeEventSink
}

// NewFirstStage validates and freezes trusted orchestration configuration.
func NewFirstStage(config FirstStageConfig) (*FirstStage, error) {
	if len(config.ProtectedBaseline) == 0 {
		return nil, errors.New("protected baseline is required")
	}
	if config.GenerateCollectionID == nil || config.Clock == nil ||
		config.StartupPrerequisite == nil || config.OutputPathPrerequisite == nil ||
		config.FingerprintProbe == nil || config.Resolve == nil || config.Finalizer == nil {
		return nil, errors.New("all first-stage seams are required")
	}
	if config.FinalizationTimeout <= 0 {
		return nil, errors.New("finalization timeout must be positive")
	}

	catalog := make(map[string]capability.Capability, len(config.CapabilityCatalog))
	for _, definition := range config.CapabilityCatalog {
		if err := definition.Validate(); err != nil {
			return nil, fmt.Errorf("validate capability catalog entry: %w", err)
		}
		if _, exists := catalog[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate capability definition %q", definition.ID)
		}
		catalog[definition.ID] = definition
	}

	baseline := make([]capability.CapabilityRequest, 0, len(config.ProtectedBaseline))
	baselineIDs := make(map[string]struct{}, len(config.ProtectedBaseline))
	for _, request := range config.ProtectedBaseline {
		if err := request.Validate(); err != nil {
			return nil, fmt.Errorf("validate protected baseline request: %w", err)
		}
		if !request.Protected {
			return nil, fmt.Errorf("protected baseline request %q must be marked protected", request.ID)
		}
		if _, exists := baselineIDs[request.ID]; exists {
			return nil, fmt.Errorf("duplicate protected baseline request %q", request.ID)
		}
		definition, exists := catalog[request.ID]
		if !exists {
			return nil, fmt.Errorf("protected baseline capability %q is not in the catalog", request.ID)
		}
		if definition.AcquisitionSemantics == capability.ActiveTrace {
			return nil, fmt.Errorf("protected baseline capability %q uses ACTIVE_TRACE", request.ID)
		}
		baselineIDs[request.ID] = struct{}{}
		baseline = append(baseline, request)
	}

	providers := make(map[string]provider.Runner, len(config.Providers))
	providerDescriptors := make(map[string]provider.Descriptor, len(config.Providers))
	descriptors := make([]provider.Descriptor, 0, len(config.Providers))
	for _, runner := range config.Providers {
		if runner == nil {
			return nil, errors.New("provider binding is nil")
		}
		descriptor := cloneDescriptor(runner.Descriptor())
		if err := descriptor.Validate(); err != nil {
			return nil, fmt.Errorf("validate provider descriptor: %w", err)
		}
		if descriptor.Class != provider.SyntheticTest {
			return nil, fmt.Errorf("first-stage slice accepts only SYNTHETIC_TEST providers, got %q", descriptor.Class)
		}
		if _, exists := providers[descriptor.ID]; exists {
			return nil, fmt.Errorf("duplicate provider identity %q", descriptor.ID)
		}
		providers[descriptor.ID] = runner
		providerDescriptors[descriptor.ID] = cloneDescriptor(descriptor)
		descriptors = append(descriptors, descriptor)
	}

	return &FirstStage{
		catalog:                catalog,
		baseline:               append([]capability.CapabilityRequest(nil), baseline...),
		providers:              providers,
		providerDescriptors:    providerDescriptors,
		descriptors:            cloneDescriptors(descriptors),
		resolverPolicy:         config.ResolverPolicy,
		generateCollectionID:   config.GenerateCollectionID,
		clock:                  config.Clock,
		startupPrerequisite:    config.StartupPrerequisite,
		outputPathPrerequisite: config.OutputPathPrerequisite,
		fingerprintProbe:       config.FingerprintProbe,
		resolve:                config.Resolve,
		finalizer:              config.Finalizer,
		finalizationTimeout:    config.FinalizationTimeout,
	}, nil
}

func (s *FirstStage) emitRuntime(event RuntimeEvent) {
	if s == nil || s.runtimeSink == nil || event.Validate() != nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = s.runtimeSink.TryEmit(event)
}

// NewCollectionID returns a cryptographically random, canonical COL-prefixed
// UUIDv4 using only the standard library.
func NewCollectionID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate collection id: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("COL-%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// Run performs one synchronous first-stage orchestration attempt.
func (s *FirstStage) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if ctx == nil {
		return RunResult{}, errors.New("run context is required")
	}
	if err := request.Validate(); err != nil {
		return RunResult{}, err
	}
	if !s.beginRun() {
		return RunResult{}, errors.New("first-stage instance already has an active Run")
	}
	defer s.endRun()

	collectionID, err := s.generateCollectionID()
	if err != nil {
		return RunResult{}, fmt.Errorf("generate collection id: %w", err)
	}
	if err := validateCollectionID(collectionID); err != nil {
		return RunResult{}, fmt.Errorf("validate generated collection id: %w", err)
	}
	started := s.clock().UTC()
	if started.IsZero() {
		return RunResult{}, errors.New("collection clock returned zero time")
	}
	collection := CollectionContext{
		CollectionID:      collectionID,
		CaseID:            request.CaseID,
		OutputDestination: request.OutputDestination,
		StartedAt:         started.Format(time.RFC3339Nano),
	}
	if err := collection.Validate(); err != nil {
		return RunResult{}, fmt.Errorf("validate collection context: %w", err)
	}

	requests := mergeRequests(s.baseline, request.Supplemental)
	result := RunResult{Context: collection, Records: s.skeletonRecords(requests)}
	s.emitRuntime(RuntimeEvent{Type: RuntimeTypeStart, Event: RuntimeEventRunStarted})
	defer s.emitRuntime(RuntimeEvent{Type: RuntimeTypeTerminal, Event: RuntimeEventRunReturned})
	if ctx.Err() != nil {
		return s.failBeforeExecution(result, OrchestrationCancelled, ctx.Err()), nil
	}
	if err := s.startupPrerequisite(ctx, collection); err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return s.failBeforeExecution(result, OrchestrationCancelled, err), nil
		}
		return s.failBeforeExecution(result, OrchestrationStartupPrerequisiteFailed, err), nil
	}
	if ctx.Err() != nil {
		return s.failBeforeExecution(result, OrchestrationCancelled, ctx.Err()), nil
	}
	if err := s.outputPathPrerequisite(collection.OutputDestination); err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return s.failBeforeExecution(result, OrchestrationCancelled, err), nil
		}
		return s.failBeforeExecution(result, OrchestrationStartupPrerequisiteFailed, err), nil
	}
	if ctx.Err() != nil {
		return s.failBeforeExecution(result, OrchestrationCancelled, ctx.Err()), nil
	}
	s.emitRuntime(RuntimeEvent{Type: RuntimeTypePhase, Event: RuntimeEventFingerprint})
	target, err := s.fingerprintProbe(ctx, collection.OutputDestination)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return s.failBeforeExecution(result, OrchestrationCancelled, err), nil
		}
		return s.failBeforeExecution(result, OrchestrationStartupPrerequisiteFailed, err), nil
	}
	if ctx.Err() != nil {
		return s.failBeforeExecution(result, OrchestrationCancelled, ctx.Err()), nil
	}
	if err := target.Validate(); err != nil {
		return s.failBeforeExecution(result, OrchestrationStartupPrerequisiteFailed, fmt.Errorf("validate target fingerprint: %w", err)), nil
	}
	if ctx.Err() != nil {
		return s.failBeforeExecution(result, OrchestrationCancelled, ctx.Err()), nil
	}
	retained := target.Clone()
	result.Fingerprint = &retained

	cancelled := false
	var realSession firstStagePackageSession
	var packageFailureErr error
	for index := range result.Records {
		if cancelled || ctx.Err() != nil {
			cancelled = true
			s.markCancelled(&result.Records[index])
			continue
		}
		record := &result.Records[index]
		if record.Capability == nil {
			record.Compatibility = execution.Unavailable
			record.OrchestrationReason = OrchestrationUnknownCapability
			record.MissingEvidence = []string{"The requested capability is not present in the trusted catalog."}
			continue
		}

		s.emitRuntime(RuntimeEvent{Type: RuntimeTypePhase, Event: RuntimeEventResolution})
		decision, resolveErr := s.resolve(
			*record.Capability,
			target.Clone(),
			cloneDescriptors(s.descriptors),
			s.resolverPolicy,
		)
		if resolveErr == nil {
			resolveErr = resolver.ValidateDecision(decision)
		}
		if resolveErr != nil {
			record.Compatibility = execution.Unavailable
			record.OrchestrationReason = OrchestrationResolutionFailed
			record.MissingEvidence = []string{"Compatibility resolution did not produce a valid decision: " + resolveErr.Error()}
			continue
		}
		decision = cloneDecision(decision)
		record.Decision = &decision
		record.Compatibility = decision.Compatibility
		if decision.Selected == nil {
			record.Execution.Reason = decision.Reason
			record.MissingEvidence = []string{"No suitable provider was available for the requested capability."}
			continue
		}

		selected := cloneDescriptor(*decision.Selected)
		trustedDescriptor, trusted := s.providerDescriptors[selected.ID]
		if !trusted || !reflect.DeepEqual(selected, trustedDescriptor) || !trustedDescriptor.Supports(record.Capability.ID) {
			record.Decision = nil
			record.Compatibility = execution.Unavailable
			record.OrchestrationReason = OrchestrationResolutionFailed
			record.MissingEvidence = []string{"The resolver selected a descriptor that is not a trusted provider binding for the requested capability."}
			continue
		}
		selected = cloneDescriptor(trustedDescriptor)
		record.SelectedProvider = &selected
		if ctx.Err() != nil {
			cancelled = true
			s.markCancelled(record)
			continue
		}
		if s.realMode {
			s.emitRuntime(RuntimeEvent{Type: RuntimeTypePhase, Event: RuntimeEventCollection})
			if err := s.runRealSelected(ctx, collection, target, record, &realSession, &cancelled); err != nil {
				if packageFailureErr == nil {
					packageFailureErr = err
				}
			}
			continue
		}
		runner, exists := s.providers[selected.ID]
		if !exists {
			record.SelectedProvider = nil
			record.Decision = nil
			record.Compatibility = execution.Unavailable
			record.OrchestrationReason = OrchestrationResolutionFailed
			record.MissingEvidence = []string{"The selected provider has no trusted execution binding."}
			continue
		}

		s.emitRuntime(RuntimeEvent{Type: RuntimeTypePhase, Event: RuntimeEventCollection})
		record.Attempted = true
		s.emitRuntime(RuntimeEvent{Type: RuntimeTypeActivity, Event: RuntimeEventProviderStarted})
		executionResult := runner.Execute(ctx, *record.Capability, target.Clone())
		s.emitRuntime(RuntimeEvent{Type: RuntimeTypeActivity, Event: RuntimeEventProviderFinished})
		if validationErr := executionResult.Validate(); validationErr != nil {
			executionResult = execution.Result{
				State:             execution.Failed,
				Reason:            execution.ReasonProviderError,
				Detail:            "Provider returned an invalid execution result: " + validationErr.Error(),
				SideEffectSummary: "Provider execution occurred, but its result failed contract validation.",
			}
		}
		record.Execution = cloneExecutionResult(executionResult)
		record.MissingEvidence = missingEvidenceFor(*record)
		if ctx.Err() != nil && executionResult.State != execution.Collected {
			cancelled = true
		}
	}
	for _, record := range result.Records {
		if err := record.Validate(); err != nil {
			return s.failFinalization(result, fmt.Errorf("validate terminal accounting for %q: %w", record.Request.ID, err)), nil
		}
	}
	if packageFailureErr != nil {
		return s.failFinalization(result, packageFailureErr), nil
	}

	s.emitRuntime(RuntimeEvent{Type: RuntimeTypeFinalizing, Event: RuntimeEventStarted})
	finalizeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.finalizationTimeout)
	defer cancel()
	var finalization FinalizationResult
	var finalizeErr error
	if s.realMode {
		finalization, finalizeErr = s.finalizeReal(finalizeContext, collection, target, result, realSession, cancelled)
	} else {
		finalization, finalizeErr = s.finalizer.Finalize(
			finalizeContext,
			collection,
			target.Clone(),
			cloneRecords(result.Records),
		)
	}
	if finalizeErr != nil {
		return s.failFinalization(result, finalizeErr), nil
	}
	s.emitRuntime(RuntimeEvent{Type: RuntimeTypeVerifying, Event: RuntimeEventStarted})
	if err := validateFinalization(finalization, result.Records); err != nil {
		return s.failFinalization(result, err), nil
	}
	applyFinalization(&result, finalization)
	result.FinalizationVerified = true
	result.PackageReference = finalization.PackageReference
	result.State = deriveRunState(result.Records)
	if cancelled && result.State == RunPartial {
		result.OrchestrationReason = OrchestrationCancelled
	}
	if err := result.Validate(); err != nil {
		return RunResult{}, fmt.Errorf("validate first-stage result: %w", err)
	}
	return result, nil
}

func (s *FirstStage) beginRun() bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runActive {
		return false
	}
	s.runActive = true
	return true
}

func (s *FirstStage) endRun() {
	s.runMu.Lock()
	s.runActive = false
	s.runMu.Unlock()
}

// Validate rejects malformed operator-owned run input before a CollectionContext
// can exist.
func (r RunRequest) Validate() error {
	if strings.TrimSpace(r.OutputDestination) == "" {
		return errors.New("output destination is required")
	}
	if err := validateCaseID(r.CaseID); err != nil {
		return err
	}
	for _, request := range r.Supplemental {
		if err := request.Validate(); err != nil {
			return fmt.Errorf("validate supplemental capability request: %w", err)
		}
		if request.Protected {
			return fmt.Errorf("supplemental capability %q cannot mark itself protected", request.ID)
		}
	}
	return nil
}

// Validate checks immutable CollectionContext fields.
func (c CollectionContext) Validate() error {
	if err := validateCollectionID(c.CollectionID); err != nil {
		return err
	}
	if err := validateCaseID(c.CaseID); err != nil {
		return err
	}
	if strings.TrimSpace(c.OutputDestination) == "" {
		return errors.New("collection output destination is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, c.StartedAt); err != nil {
		return fmt.Errorf("collection started_at is invalid: %w", err)
	}
	return nil
}

// Validate enforces application-owned reason and attempted-execution domains.
func (r CapabilityRecord) Validate() error {
	if err := r.Request.Validate(); err != nil {
		return err
	}
	if !r.Compatibility.Valid() {
		return errors.New("capability record compatibility is invalid")
	}
	if err := r.Execution.Validate(); err != nil {
		return fmt.Errorf("validate capability execution accounting: %w", err)
	}
	if r.Capability != nil {
		if err := r.Capability.Validate(); err != nil {
			return fmt.Errorf("validate capability record definition: %w", err)
		}
		if r.Capability.ID != r.Request.ID {
			return errors.New("capability definition does not match the request")
		}
	}
	if r.Decision != nil {
		if err := resolver.ValidateDecision(*r.Decision); err != nil {
			return fmt.Errorf("validate capability record decision: %w", err)
		}
		if r.Decision.Compatibility != r.Compatibility {
			return errors.New("capability record compatibility does not match the decision")
		}
		if r.Decision.Selected != nil && r.SelectedProvider == nil {
			return errors.New("resolver-selected provider is missing from capability accounting")
		}
		if r.Decision.Selected != nil && !r.Decision.Selected.Supports(r.Request.ID) {
			return errors.New("resolver-selected provider does not support the requested capability")
		}
	}
	if r.SelectedProvider != nil {
		if err := r.SelectedProvider.Validate(); err != nil {
			return fmt.Errorf("validate selected provider: %w", err)
		}
		if r.Decision == nil || r.Decision.Selected == nil || r.Decision.Selected.ID != r.SelectedProvider.ID {
			return errors.New("selected provider does not match a resolver decision")
		}
		if r.Capability == nil || !r.SelectedProvider.Supports(r.Request.ID) {
			return errors.New("selected provider does not support the requested capability")
		}
	}
	if r.Attempted && (r.Capability == nil || r.SelectedProvider == nil || r.Decision == nil) {
		return errors.New("attempted execution requires capability, decision, and selected provider")
	}
	if !r.Attempted && r.Execution.State != execution.Skipped {
		return errors.New("non-attempted accounting must use SKIPPED execution")
	}
	if r.SelectedProvider != nil && !r.Attempted && r.OrchestrationReason != OrchestrationCancelled && !isPreAttemptPackageFailureRecord(r) {
		return errors.New("selected non-attempted accounting requires CANCELLED or exact package-preparation failure")
	}
	if r.OrchestrationReason != "" {
		if !r.OrchestrationReason.validForCapabilityRecord() {
			return fmt.Errorf("orchestration reason %q is invalid for a capability record", r.OrchestrationReason)
		}
		if r.Attempted || r.Execution.State != execution.Skipped || r.Execution.Reason != execution.ReasonNone {
			return errors.New("application-owned capability reason requires non-attempted SKIPPED/NONE accounting")
		}
	}
	if r.OrchestrationReason == OrchestrationUnknownCapability && (r.Capability != nil || r.Decision != nil || r.SelectedProvider != nil) {
		return errors.New("unknown capability accounting must not fabricate capability, decision, or provider")
	}
	if r.OrchestrationReason == OrchestrationUnknownCapability && r.Compatibility != execution.Unavailable {
		return errors.New("unknown capability accounting must use UNAVAILABLE compatibility")
	}
	if r.OrchestrationReason == OrchestrationResolutionFailed && (r.Capability == nil || r.Decision != nil || r.SelectedProvider != nil) {
		return errors.New("resolution failure requires a known capability and no decision or provider")
	}
	if r.OrchestrationReason == OrchestrationResolutionFailed && r.Compatibility != execution.Unavailable {
		return errors.New("resolution failure accounting must use UNAVAILABLE compatibility")
	}
	if r.OrchestrationReason == OrchestrationPackageFinalizationFailed && !isPreAttemptPackageFailureRecord(r) {
		return errors.New("capability package-finalization reason is reserved for exact pre-attempt package preparation failure")
	}
	for _, summary := range r.MissingEvidence {
		if strings.TrimSpace(summary) == "" {
			return errors.New("missing evidence summary must be non-whitespace")
		}
	}
	if !recordFullySatisfied(r) && len(r.MissingEvidence) == 0 {
		return errors.New("unsatisfied capability record requires missing evidence")
	}
	if r.ArtifactReference != "" && strings.TrimSpace(r.ReceiptReference) == "" {
		return errors.New("artifact reference requires a receipt reference")
	}
	return nil
}

// Validate enforces run-level reason, accounting, and finalization invariants.
func (r RunResult) Validate() error {
	if err := r.Context.Validate(); err != nil {
		return err
	}
	if !r.State.valid() {
		return errors.New("run state is invalid")
	}
	if !r.OrchestrationReason.validForRunResult() {
		return fmt.Errorf("orchestration reason %q is invalid for a run result", r.OrchestrationReason)
	}
	if len(r.Records) == 0 {
		return errors.New("run result requires capability records")
	}
	seen := make(map[string]struct{}, len(r.Records))
	for _, record := range r.Records {
		if _, exists := seen[record.Request.ID]; exists {
			return fmt.Errorf("duplicate capability record %q", record.Request.ID)
		}
		seen[record.Request.ID] = struct{}{}
		if err := record.Validate(); err != nil {
			return fmt.Errorf("validate capability record %q: %w", record.Request.ID, err)
		}
	}
	if r.State == RunFailed {
		if r.OrchestrationReason == "" {
			return errors.New("failed run requires an application-owned reason")
		}
		if r.FinalizationVerified || r.PackageReference != "" {
			return errors.New("failed run must not claim verified finalization")
		}
		if r.OrchestrationReason == OrchestrationPackageFinalizationFailed && r.Fingerprint == nil {
			return errors.New("package finalization failure requires a successfully acquired fingerprint")
		}
		if (r.OrchestrationReason == OrchestrationStartupPrerequisiteFailed || r.OrchestrationReason == OrchestrationCancelled) && r.Fingerprint != nil {
			return errors.New("pre-execution run failure must not claim a retained fingerprint")
		}
		return nil
	}
	if r.Fingerprint == nil {
		return errors.New("complete or partial run requires a fingerprint")
	}
	if err := r.Fingerprint.Validate(); err != nil {
		return fmt.Errorf("validate run fingerprint: %w", err)
	}
	if !r.FinalizationVerified || strings.TrimSpace(r.PackageReference) == "" {
		return errors.New("complete or partial run requires verified finalization")
	}
	for _, record := range r.Records {
		if strings.TrimSpace(record.ReceiptReference) == "" {
			return fmt.Errorf("capability record %q has no receipt reference", record.Request.ID)
		}
	}
	if r.State == RunComplete {
		if r.OrchestrationReason != "" {
			return errors.New("complete run must not carry an orchestration reason")
		}
		for _, record := range r.Records {
			if !recordFullySatisfied(record) {
				return errors.New("complete run contains unsatisfied capability accounting")
			}
		}
		return nil
	}
	if r.OrchestrationReason != "" && r.OrchestrationReason != OrchestrationCancelled {
		return errors.New("partial run may only carry CANCELLED as a run reason")
	}
	for _, record := range r.Records {
		if !recordFullySatisfied(record) {
			return nil
		}
	}
	return errors.New("partial run has no degraded, partial, or missing outcome")
}

func (r OrchestrationReason) validForCapabilityRecord() bool {
	switch r {
	case OrchestrationUnknownCapability, OrchestrationResolutionFailed, OrchestrationCancelled, OrchestrationPackageFinalizationFailed:
		return true
	default:
		return false
	}
}

func isPreAttemptPackageFailureRecord(r CapabilityRecord) bool {
	return r.SelectedProvider != nil &&
		r.Decision != nil &&
		r.Decision.Selected != nil &&
		r.Decision.Selected.ID == r.SelectedProvider.ID &&
		r.Capability != nil &&
		r.SelectedProvider.Supports(r.Request.ID) &&
		r.Compatibility == r.Decision.Compatibility &&
		!r.Attempted &&
		r.Execution.State == execution.Skipped &&
		r.Execution.Reason == execution.ReasonNone &&
		r.OrchestrationReason == OrchestrationPackageFinalizationFailed &&
		len(r.MissingEvidence) == 1 &&
		r.MissingEvidence[0] == receipt.FirstStagePackagePreparationMissingEvidence &&
		r.ArtifactReference == "" &&
		r.ReceiptReference == ""
}

func (r OrchestrationReason) validForRunResult() bool {
	switch r {
	case "", OrchestrationStartupPrerequisiteFailed, OrchestrationCancelled, OrchestrationPackageFinalizationFailed:
		return true
	default:
		return false
	}
}

func (s RunState) valid() bool {
	return s == RunComplete || s == RunPartial || s == RunFailed
}

func validateCaseID(value string) error {
	if value == "" {
		return nil
	}
	if !utf8.ValidString(value) {
		return errors.New("case id must contain valid UTF-8")
	}
	count := utf8.RuneCountInString(value)
	if count < 1 || count > 128 || strings.TrimSpace(value) == "" {
		return errors.New("case id must contain 1 to 128 meaningful characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("case id must not contain control characters")
		}
	}
	return nil
}

func validateCollectionID(value string) error {
	if len(value) != 40 || !strings.HasPrefix(value, "COL-") {
		return errors.New("collection id must be a COL-prefixed canonical UUIDv4")
	}
	uuid := value[4:]
	if uuid != strings.ToLower(uuid) || uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
		return errors.New("collection id must contain a lowercase canonical UUID")
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil || len(raw) != 16 {
		return errors.New("collection id contains invalid UUID bytes")
	}
	if raw[6]>>4 != 4 || raw[8]>>6 != 2 {
		return errors.New("collection id must use UUIDv4 version and RFC variant bits")
	}
	return nil
}

func mergeRequests(baseline, supplemental []capability.CapabilityRequest) []capability.CapabilityRequest {
	merged := make(map[string]capability.CapabilityRequest, len(baseline)+len(supplemental))
	for _, request := range baseline {
		merged[request.ID] = request
	}
	for _, request := range supplemental {
		existing, exists := merged[request.ID]
		if !exists {
			merged[request.ID] = request
			continue
		}
		if request.Priority.Order() < existing.Priority.Order() {
			existing.Priority = request.Priority
		}
		existing.Protected = existing.Protected || request.Protected
		merged[request.ID] = existing
	}
	ordered := make([]capability.CapabilityRequest, 0, len(merged))
	for _, request := range merged {
		ordered = append(ordered, request)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority.Order() < ordered[j].Priority.Order()
		}
		if ordered[i].Protected != ordered[j].Protected {
			return ordered[i].Protected
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}

func (s *FirstStage) skeletonRecords(requests []capability.CapabilityRequest) []CapabilityRecord {
	records := make([]CapabilityRecord, 0, len(requests))
	for _, request := range requests {
		record := CapabilityRecord{
			Request:       request,
			Compatibility: execution.Unavailable,
			Execution:     unexecutedResult(execution.ReasonNone),
		}
		if definition, exists := s.catalog[request.ID]; exists {
			copy := definition
			record.Capability = &copy
		}
		records = append(records, record)
	}
	return records
}

func unexecutedResult(reason execution.Reason) execution.Result {
	return execution.Result{
		State:             execution.Skipped,
		Reason:            reason,
		Detail:            "No provider was executed.",
		SideEffectSummary: "No provider was executed.",
	}
}

func (s *FirstStage) failBeforeExecution(result RunResult, reason OrchestrationReason, cause error) RunResult {
	result.State = RunFailed
	result.OrchestrationReason = reason
	if cause != nil {
		result.Detail = cause.Error()
	}
	for index := range result.Records {
		result.Records[index].Compatibility = execution.Unavailable
		result.Records[index].Attempted = false
		result.Records[index].Execution = unexecutedResult(execution.ReasonNone)
		result.Records[index].MissingEvidence = []string{"The run ended before capability resolution and execution completed."}
	}
	return result
}

func (s *FirstStage) failFinalization(result RunResult, cause error) RunResult {
	result.State = RunFailed
	result.OrchestrationReason = OrchestrationPackageFinalizationFailed
	result.FinalizationVerified = false
	result.PackageReference = ""
	if cause != nil {
		result.Detail = cause.Error()
	}
	return result
}

func (s *FirstStage) markCancelled(record *CapabilityRecord) {
	record.Attempted = false
	record.Execution = unexecutedResult(execution.ReasonNone)
	record.OrchestrationReason = OrchestrationCancelled
	record.MissingEvidence = []string{"The request was not executed because the run was cancelled."}
}

func missingEvidenceFor(record CapabilityRecord) []string {
	missing := make([]string, 0, 2)
	if record.Compatibility != execution.Available {
		missing = append(missing, "Provider compatibility was not fully available.")
	}
	if record.Execution.State != execution.Collected {
		missing = append(missing, "Provider execution did not fully collect the requested evidence.")
	}
	return missing
}

func recordFullySatisfied(record CapabilityRecord) bool {
	return record.Compatibility == execution.Available &&
		record.Execution.State == execution.Collected &&
		len(record.MissingEvidence) == 0
}

func deriveRunState(records []CapabilityRecord) RunState {
	for _, record := range records {
		if !recordFullySatisfied(record) {
			return RunPartial
		}
	}
	return RunComplete
}

func validateFinalization(result FinalizationResult, records []CapabilityRecord) error {
	if !result.Verified {
		return errors.New("finalizer did not verify the terminal package")
	}
	if strings.TrimSpace(result.PackageReference) == "" {
		return errors.New("verified finalization requires a package reference")
	}
	if len(result.References) != len(records) {
		return errors.New("verified finalization requires exactly one reference per capability record")
	}
	expected := make(map[string]struct{}, len(records))
	for _, record := range records {
		expected[record.Request.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(result.References))
	seenReceipts := make(map[string]struct{}, len(result.References))
	for _, reference := range result.References {
		if _, exists := expected[reference.CapabilityID]; !exists {
			return fmt.Errorf("finalizer returned unknown capability reference %q", reference.CapabilityID)
		}
		if _, exists := seen[reference.CapabilityID]; exists {
			return fmt.Errorf("finalizer returned duplicate capability reference %q", reference.CapabilityID)
		}
		if strings.TrimSpace(reference.ReceiptReference) == "" {
			return fmt.Errorf("finalizer returned empty receipt reference for %q", reference.CapabilityID)
		}
		receiptKey := strings.TrimSpace(reference.ReceiptReference)
		if _, exists := seenReceipts[receiptKey]; exists {
			return fmt.Errorf("finalizer returned duplicate receipt reference %q", reference.ReceiptReference)
		}
		seenReceipts[receiptKey] = struct{}{}
		seen[reference.CapabilityID] = struct{}{}
	}
	return nil
}

func applyFinalization(run *RunResult, result FinalizationResult) {
	references := make(map[string]FinalizationReference, len(result.References))
	for _, reference := range result.References {
		references[reference.CapabilityID] = reference
	}
	for index := range run.Records {
		reference := references[run.Records[index].Request.ID]
		run.Records[index].ReceiptReference = reference.ReceiptReference
		run.Records[index].ArtifactReference = reference.ArtifactReference
	}
}

func cloneRecords(records []CapabilityRecord) []CapabilityRecord {
	clones := make([]CapabilityRecord, len(records))
	for index, record := range records {
		clone := record
		if record.Capability != nil {
			definition := *record.Capability
			clone.Capability = &definition
		}
		if record.Decision != nil {
			decision := cloneDecision(*record.Decision)
			clone.Decision = &decision
		}
		if record.SelectedProvider != nil {
			descriptor := cloneDescriptor(*record.SelectedProvider)
			clone.SelectedProvider = &descriptor
		}
		clone.Execution = cloneExecutionResult(record.Execution)
		clone.MissingEvidence = append([]string(nil), record.MissingEvidence...)
		clones[index] = clone
	}
	return clones
}

func cloneExecutionResult(result execution.Result) execution.Result {
	clone := result
	clone.Payload = append([]byte(nil), result.Payload...)
	return clone
}

func cloneDecision(decision resolver.Decision) resolver.Decision {
	clone := decision
	if decision.Selected != nil {
		selected := cloneDescriptor(*decision.Selected)
		clone.Selected = &selected
	}
	clone.Evaluations = append([]resolver.CandidateEvaluation(nil), decision.Evaluations...)
	return clone
}

func cloneDescriptors(descriptors []provider.Descriptor) []provider.Descriptor {
	clones := make([]provider.Descriptor, len(descriptors))
	for index, descriptor := range descriptors {
		clones[index] = cloneDescriptor(descriptor)
	}
	return clones
}

func cloneDescriptor(descriptor provider.Descriptor) provider.Descriptor {
	clone := descriptor
	clone.Capabilities = append([]string(nil), descriptor.Capabilities...)
	clone.Requirements.Platforms = append([]string(nil), descriptor.Requirements.Platforms...)
	clone.Requirements.OSFamilies = append([]string(nil), descriptor.Requirements.OSFamilies...)
	clone.Requirements.Architectures = append([]string(nil), descriptor.Requirements.Architectures...)
	clone.Requirements.RuntimeLanes = append([]string(nil), descriptor.Requirements.RuntimeLanes...)
	clone.SideEffects = append([]string(nil), descriptor.SideEffects...)
	return clone
}
