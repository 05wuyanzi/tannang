// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/evidence"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/receipt"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

const processIdentitySnapshotProviderID = "windows-toolhelp-process-snapshot"

// processIdentitySnapshotFirstStageDeps is intentionally unexported and
// limited to deterministic test substitutions. Production callers cannot
// replace Provider, Resolver, catalog, baseline policy, or filesystem sinks.
type processIdentitySnapshotFirstStageDeps struct {
	StreamingRunner    provider.StreamingRunner
	HostIdentityRunner provider.StreamingRunner
	TransportRunner    provider.StreamingRunner
	EventLogRunner     provider.FileArtifactRunner
	FingerprintProbe   func(context.Context, string) (fingerprint.TargetFingerprint, error)
	PackageFactory     firstStagePackageFactory
	Clock              func() time.Time
	CollectionID       func() (string, error)
	RuntimeSink        RuntimeEventSink
}

// NewProcessIdentitySnapshotFirstStage installs the fixed production binding.
func NewProcessIdentitySnapshotFirstStage(finalizationTimeout time.Duration) (*FirstStage, error) {
	return NewProcessIdentitySnapshotFirstStageWithRuntimeSink(finalizationTimeout, nil)
}

// NewProcessIdentitySnapshotFirstStageWithRuntimeSink installs the fixed
// production binding with optional non-authoritative runtime observations.
func NewProcessIdentitySnapshotFirstStageWithRuntimeSink(finalizationTimeout time.Duration, sink RuntimeEventSink) (*FirstStage, error) {
	if finalizationTimeout <= 0 {
		return nil, errors.New("finalization timeout must be positive")
	}
	return newProcessIdentitySnapshotFirstStageWithDeps(finalizationTimeout, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: provider.NewProcessIdentitySnapshotRunner(),
		FingerprintProbe: func(ctx context.Context, output string) (fingerprint.TargetFingerprint, error) {
			return fingerprint.Probe(ctx, output, fingerprint.Options{IncludeCPUPressure: false})
		},
		PackageFactory: defaultFirstStagePackageFactory,
		Clock:          time.Now,
		CollectionID:   NewCollectionID,
		RuntimeSink:    sink,
	})
}

// NewProcessIdentitySnapshotAndEventLogFirstStage installs the protected
// process baseline plus the fixed protected System Event Log binding.
func NewProcessIdentitySnapshotAndEventLogFirstStage(finalizationTimeout time.Duration) (*FirstStage, error) {
	return NewProcessIdentitySnapshotAndEventLogFirstStageWithRuntimeSink(finalizationTimeout, nil)
}

func NewProcessIdentitySnapshotAndEventLogFirstStageWithRuntimeSink(finalizationTimeout time.Duration, sink RuntimeEventSink) (*FirstStage, error) {
	if finalizationTimeout <= 0 {
		return nil, errors.New("finalization timeout must be positive")
	}
	return newProcessIdentitySnapshotFirstStageWithDeps(finalizationTimeout, processIdentitySnapshotFirstStageDeps{
		StreamingRunner:    provider.NewProcessIdentitySnapshotRunner(),
		HostIdentityRunner: provider.NewWindowsHostOSIdentityRunner(),
		TransportRunner:    provider.NewWindowsTransportEndpointRunner(),
		EventLogRunner:     provider.NewWindowsEventLogSystemRunner(),
		FingerprintProbe: func(ctx context.Context, output string) (fingerprint.TargetFingerprint, error) {
			return fingerprint.Probe(ctx, output, fingerprint.Options{IncludeCPUPressure: false})
		},
		PackageFactory: depsDefaultPackageFactory(), Clock: time.Now, CollectionID: NewCollectionID, RuntimeSink: sink,
	})
}

func depsDefaultPackageFactory() firstStagePackageFactory { return defaultFirstStagePackageFactory }

func newProcessIdentitySnapshotFirstStageWithDeps(finalizationTimeout time.Duration, deps processIdentitySnapshotFirstStageDeps) (*FirstStage, error) {
	if finalizationTimeout <= 0 {
		return nil, errors.New("finalization timeout must be positive")
	}
	if isNilDependency(deps.StreamingRunner) || deps.FingerprintProbe == nil || deps.PackageFactory == nil || deps.Clock == nil || deps.CollectionID == nil {
		return nil, errors.New("all real first-stage private dependencies are required")
	}
	capabilityDefinition := capability.ProcessIdentitySnapshot()
	if err := capabilityDefinition.Validate(); err != nil {
		return nil, fmt.Errorf("validate process identity snapshot capability: %w", err)
	}
	descriptor := cloneDescriptor(deps.StreamingRunner.Descriptor())
	if err := descriptor.Validate(); err != nil {
		return nil, fmt.Errorf("validate process identity snapshot provider descriptor: %w", err)
	}
	if descriptor.ID != processIdentitySnapshotProviderID || descriptor.Class != provider.FirstPartyNative || !descriptor.Supports(capabilityDefinition.ID) {
		return nil, errors.New("real first-stage provider binding is not the fixed process identity snapshot Provider")
	}
	artifact := deps.StreamingRunner.Artifact()
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("validate process identity snapshot artifact descriptor: %w", err)
	}
	if artifact.MediaType != receipt.FirstStageArtifactMedia || artifact.ContentSchemaID != receipt.FirstStageArtifactSchema {
		return nil, errors.New("real first-stage artifact binding is not the fixed process identity snapshot contract")
	}
	request := capability.CapabilityRequest{ID: capabilityDefinition.ID, Priority: capability.PriorityNormal, Protected: true}
	if err := request.Validate(); err != nil {
		return nil, fmt.Errorf("validate real protected baseline request: %w", err)
	}
	catalog := map[string]capability.Capability{capabilityDefinition.ID: capabilityDefinition}
	providerDescriptors := map[string]provider.Descriptor{descriptor.ID: cloneDescriptor(descriptor)}
	stage := &FirstStage{
		catalog:                catalog,
		baseline:               []capability.CapabilityRequest{request},
		providerDescriptors:    providerDescriptors,
		descriptors:            []provider.Descriptor{cloneDescriptor(descriptor)},
		resolverPolicy:         resolver.Policy{AllowActiveTrace: false},
		generateCollectionID:   deps.CollectionID,
		clock:                  deps.Clock,
		startupPrerequisite:    func(ctx context.Context, _ CollectionContext) error { return ctx.Err() },
		outputPathPrerequisite: evidence.ValidateOutputPath,
		fingerprintProbe:       deps.FingerprintProbe,
		resolve:                resolver.Resolve,
		finalizationTimeout:    finalizationTimeout,
		realMode:               true,
		streamingRunner:        deps.StreamingRunner,
		streamingDescriptor:    cloneDescriptor(descriptor),
		streamingRunners:       map[string]provider.StreamingRunner{capabilityDefinition.ID: deps.StreamingRunner},
		packageFactory:         deps.PackageFactory,
		runtimeSink:            deps.RuntimeSink,
		fileRunners:            make(map[string]provider.FileArtifactRunner),
		availabilityProbers:    make(map[string]provider.AvailabilityProber),
	}
	if deps.EventLogRunner != nil {
		eventDescriptor := cloneDescriptor(deps.EventLogRunner.Descriptor())
		if err := eventDescriptor.Validate(); err != nil {
			return nil, fmt.Errorf("validate Event Log provider descriptor: %w", err)
		}
		if eventDescriptor.ID != provider.WindowsEventLogSystemProviderID || eventDescriptor.Class != provider.FirstPartyNative || !eventDescriptor.Supports(capability.WindowsEventLogSystemChannelID) {
			return nil, errors.New("real Event Log provider binding is not the fixed System channel Provider")
		}
		eventArtifact := deps.EventLogRunner.Artifact()
		if err := eventArtifact.Validate(); err != nil {
			return nil, fmt.Errorf("validate Event Log artifact descriptor: %w", err)
		}
		if eventArtifact.MediaType != receipt.WindowsEventLogSystemMediaType || eventArtifact.ContentSchemaID != receipt.WindowsEventLogSystemSchemaID {
			return nil, errors.New("real Event Log artifact binding is not the fixed EVTX contract")
		}
		definition := capability.WindowsEventLogSystemChannel()
		catalog[definition.ID] = definition
		providerDescriptors[eventDescriptor.ID] = cloneDescriptor(eventDescriptor)
		stage.catalog[definition.ID] = definition
		stage.providerDescriptors[eventDescriptor.ID] = cloneDescriptor(eventDescriptor)
		stage.descriptors = append(stage.descriptors, cloneDescriptor(eventDescriptor))
		stage.fileRunners[definition.ID] = deps.EventLogRunner
		eventRequest := capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityLate, Protected: true}
		if err := eventRequest.Validate(); err != nil {
			return nil, fmt.Errorf("validate protected Event Log baseline request: %w", err)
		}
		stage.baseline = append(stage.baseline, eventRequest)
		if probe, ok := deps.EventLogRunner.(provider.AvailabilityProber); ok {
			stage.availabilityProbers[definition.ID] = probe
		}
	}
	if deps.HostIdentityRunner != nil {
		hostDescriptor := cloneDescriptor(deps.HostIdentityRunner.Descriptor())
		if err := hostDescriptor.Validate(); err != nil {
			return nil, fmt.Errorf("validate host identity provider descriptor: %w", err)
		}
		if hostDescriptor.ID != provider.WindowsHostOSIdentityProviderID || hostDescriptor.Class != provider.FirstPartyNative || !hostDescriptor.Supports(capability.WindowsHostOSIdentitySnapshotID) {
			return nil, errors.New("real host identity provider binding is not the fixed host identity Provider")
		}
		hostArtifact := deps.HostIdentityRunner.Artifact()
		if err := hostArtifact.Validate(); err != nil {
			return nil, fmt.Errorf("validate host identity artifact descriptor: %w", err)
		}
		if hostArtifact.MediaType != provider.WindowsHostOSIdentityMediaType || hostArtifact.ContentSchemaID != provider.WindowsHostOSIdentitySchemaID {
			return nil, errors.New("real host identity artifact binding is not the fixed JSON contract")
		}
		definition := capability.WindowsHostOSIdentitySnapshot()
		stage.catalog[definition.ID] = definition
		stage.providerDescriptors[hostDescriptor.ID] = cloneDescriptor(hostDescriptor)
		stage.descriptors = append(stage.descriptors, cloneDescriptor(hostDescriptor))
		stage.streamingRunners[definition.ID] = deps.HostIdentityRunner
		hostRequest := capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityLate, Protected: true}
		if err := hostRequest.Validate(); err != nil {
			return nil, fmt.Errorf("validate protected Host/OS Identity baseline request: %w", err)
		}
		stage.baseline = append(stage.baseline, hostRequest)
	}
	if deps.TransportRunner != nil {
		transportDescriptor := cloneDescriptor(deps.TransportRunner.Descriptor())
		if err := transportDescriptor.Validate(); err != nil {
			return nil, fmt.Errorf("validate transport endpoint provider descriptor: %w", err)
		}
		if transportDescriptor.ID != provider.WindowsTransportEndpointProviderID || transportDescriptor.Class != provider.FirstPartyNative || !transportDescriptor.Supports(capability.WindowsTransportEndpointSnapshotID) {
			return nil, errors.New("real transport endpoint provider binding is not the fixed transport endpoint Provider")
		}
		transportArtifact := deps.TransportRunner.Artifact()
		if err := transportArtifact.Validate(); err != nil {
			return nil, fmt.Errorf("validate transport endpoint artifact descriptor: %w", err)
		}
		if transportArtifact.MediaType != provider.WindowsTransportEndpointMediaType || transportArtifact.ContentSchemaID != provider.WindowsTransportEndpointSchemaID {
			return nil, errors.New("real transport endpoint artifact binding is not the fixed transport endpoint contract")
		}
		definition := capability.WindowsTransportEndpointSnapshot()
		stage.catalog[definition.ID] = definition
		stage.providerDescriptors[transportDescriptor.ID] = cloneDescriptor(transportDescriptor)
		stage.descriptors = append(stage.descriptors, cloneDescriptor(transportDescriptor))
		stage.streamingRunners[definition.ID] = deps.TransportRunner
		probe, ok := deps.TransportRunner.(provider.AvailabilityProber)
		if !ok {
			return nil, errors.New("real transport endpoint Provider must implement AvailabilityProber")
		}
		stage.availabilityProbers[definition.ID] = probe
		transportRequest := capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityLate, Protected: true}
		if err := transportRequest.Validate(); err != nil {
			return nil, fmt.Errorf("validate protected transport endpoint baseline request: %w", err)
		}
		stage.baseline = append(stage.baseline, transportRequest)
	}
	return stage, nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func (s *FirstStage) validateRealBinding() error {
	if s == nil || !s.realMode || s.streamingRunner == nil || s.packageFactory == nil || len(s.streamingRunners) == 0 {
		return errors.New("real first-stage binding is not initialized")
	}
	if strings.TrimSpace(s.streamingDescriptor.ID) == "" || s.streamingDescriptor.Class != provider.FirstPartyNative {
		return errors.New("real first-stage descriptor is invalid")
	}
	return nil
}
