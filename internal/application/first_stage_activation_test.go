// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
)

type fakeEventLogFileRunner struct{}

func (fakeEventLogFileRunner) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: provider.WindowsEventLogSystemProviderID, Class: provider.FirstPartyNative, Capabilities: []string{capability.WindowsEventLogSystemChannelID}, Requirements: provider.Requirements{Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"}, Architectures: []string{"amd64", "x86"}, Available: true, AvailabilityReason: execution.ReasonNone}, Quality: provider.Quality{Compatibility: execution.Available, Reason: execution.ReasonNone, Fidelity: 5, Disturbance: 1, Completeness: 5, OutputStability: 5, EvidenceValue: 5}}
}
func (fakeEventLogFileRunner) Artifact() provider.ArtifactDescriptor {
	return provider.ArtifactDescriptor{MediaType: provider.WindowsEventLogSystemMediaType, ContentSchemaID: provider.WindowsEventLogSystemSchemaID}
}
func (fakeEventLogFileRunner) Probe(context.Context, fingerprint.TargetFingerprint) (execution.Reason, error) {
	return execution.ReasonNone, nil
}
func (fakeEventLogFileRunner) ExecuteToPath(_ context.Context, _ capability.Capability, _ fingerprint.TargetFingerprint, path string) execution.Result {
	if err := os.WriteFile(path, []byte("EVTX-HEADER-ONLY"), 0o600); err != nil {
		return execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, Detail: err.Error(), SideEffectSummary: "fake file write failed"}
	}
	return execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "fake Event Log export completed"}
}

type typedNilStreamingRunner struct{}

func (*typedNilStreamingRunner) Descriptor() provider.Descriptor {
	panic("Descriptor must not be called")
}
func (*typedNilStreamingRunner) Artifact() provider.ArtifactDescriptor {
	panic("Artifact must not be called")
}
func (*typedNilStreamingRunner) ExecuteTo(context.Context, capability.Capability, fingerprint.TargetFingerprint, io.Writer) execution.Result {
	panic("ExecuteTo must not be called")
}

func TestProductionProcessIdentitySnapshotBindingOracleDoesNotRun(t *testing.T) {
	stage, err := NewProcessIdentitySnapshotFirstStage(time.Second)
	if err != nil {
		t.Fatalf("production constructor error: %v", err)
	}
	if !stage.realMode || stage.streamingRunner == nil || stage.packageFactory == nil || stage.finalizer != nil {
		t.Fatalf("production real mode wiring is incomplete: %+v", stage)
	}
	if stage.streamingDescriptor.ID != processIdentitySnapshotProviderID || stage.streamingDescriptor.Class != provider.FirstPartyNative || !stage.streamingDescriptor.Supports(capability.ProcessIdentitySnapshotID) {
		t.Fatalf("unexpected production descriptor: %+v", stage.streamingDescriptor)
	}
	if len(stage.catalog) != 1 || len(stage.baseline) != 1 || stage.baseline[0].ID != capability.ProcessIdentitySnapshotID || stage.baseline[0].Priority != capability.PriorityNormal || !stage.baseline[0].Protected {
		t.Fatalf("unexpected production catalog/baseline: catalog=%+v baseline=%+v", stage.catalog, stage.baseline)
	}
	// Constructing and inspecting the binding is the entire oracle. Run,
	// FingerprintProbe and ExecuteTo are deliberately not invoked.
}

func TestProductionProcessIdentitySnapshotConstructorRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		if _, err := NewProcessIdentitySnapshotFirstStage(timeout); err == nil {
			t.Fatalf("timeout %s unexpectedly accepted", timeout)
		}
	}
}

func TestPrivateRealConstructorRejectsTypedNilStreamingRunner(t *testing.T) {
	var concrete *typedNilStreamingRunner
	_, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{StreamingRunner: concrete})
	if err == nil || !strings.Contains(err.Error(), "all real first-stage private dependencies are required") {
		t.Fatalf("typed-nil streaming runner error = %v", err)
	}
}

func TestRealProtectedBaselineCannotBeRemovedBySupplementalRequest(t *testing.T) {
	runner := newFakeStreamingRunner()
	session := &fakeFirstStageSession{}
	factory := &fakeFirstStageFactory{session: session}
	stage := newRealTestStage(t, runner, factory.begin)
	request := realRunRequest(t)
	request.Supplemental = []capability.CapabilityRequest{{ID: capability.ProcessIdentitySnapshotID, Priority: capability.PriorityEarly}}
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || !result.Records[0].Request.Protected || result.Records[0].Request.Priority != capability.PriorityEarly {
		t.Fatalf("protected baseline was removed or downgraded: %+v", result.Records)
	}
}

func TestProductionBindingPromotesEventLogIntoProtectedBaseline(t *testing.T) {
	process := newFakeStreamingRunner()
	event := fakeEventLogFileRunner{}
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: process, EventLogRunner: event,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "test", Build: "1", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN"}, nil
		},
		PackageFactory: defaultFirstStagePackageFactory, Clock: time.Now, CollectionID: NewCollectionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stage.baseline) != 2 || len(stage.catalog) != 2 ||
		stage.baseline[0].ID != capability.ProcessIdentitySnapshotID || !stage.baseline[0].Protected ||
		stage.baseline[1].ID != capability.WindowsEventLogSystemChannelID || !stage.baseline[1].Protected {
		t.Fatalf("baseline/catalog=%+v/%+v", stage.baseline, stage.catalog)
	}
	output := filepath.Join(t.TempDir(), "package")
	result, err := stage.Run(context.Background(), RunRequest{OutputDestination: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || len(result.Records) != 2 || !result.FinalizationVerified {
		t.Fatalf("result=%+v", result)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(output, "meta", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metadataBytes), `"schema_version": "1.1"`) || !strings.Contains(string(metadataBytes), `windows-event-log-system.evtx`) {
		t.Fatalf("metadata=%s", metadataBytes)
	}
}

func TestProductionBindingCatalogsHostIdentityWithoutPromotingIt(t *testing.T) {
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: newFakeStreamingRunner(), HostIdentityRunner: newFakeHostStreamingRunner(), EventLogRunner: fakeEventLogFileRunner{},
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "test", Build: "1", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN"}, nil
		},
		PackageFactory: defaultFirstStagePackageFactory, Clock: time.Now, CollectionID: NewCollectionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stage.catalog) != 3 || len(stage.baseline) != 2 || len(stage.streamingRunners) != 2 {
		t.Fatalf("catalog=%d baseline=%d streaming=%d", len(stage.catalog), len(stage.baseline), len(stage.streamingRunners))
	}
	if _, ok := stage.catalog[capability.WindowsHostOSIdentitySnapshotID]; !ok {
		t.Fatal("host identity capability is not in trusted catalog")
	}
	for _, request := range stage.baseline {
		if request.ID == capability.WindowsHostOSIdentitySnapshotID || !request.Protected {
			t.Fatalf("host identity was promoted into baseline: %+v", stage.baseline)
		}
	}
	output := filepath.Join(t.TempDir(), "package")
	result, err := stage.Run(context.Background(), RunRequest{OutputDestination: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || len(result.Records) != 2 {
		t.Fatalf("default run=%+v", result)
	}
}

func TestPromotedEventLogBaselineCannotBeRemovedOrDuplicated(t *testing.T) {
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: newFakeStreamingRunner(), EventLogRunner: fakeEventLogFileRunner{},
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "test", Build: "1", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN"}, nil
		},
		PackageFactory: defaultFirstStagePackageFactory, Clock: time.Now, CollectionID: NewCollectionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "package")
	result, err := stage.Run(context.Background(), RunRequest{OutputDestination: output, Supplemental: []capability.CapabilityRequest{{ID: capability.WindowsEventLogSystemChannelID, Priority: capability.PriorityEarly}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || result.Records[0].Request.ID != capability.WindowsEventLogSystemChannelID || result.Records[1].Request.ID != capability.ProcessIdentitySnapshotID {
		t.Fatalf("promoted baseline merge=%+v", result.Records)
	}
	for _, record := range result.Records {
		if !record.Request.Protected {
			t.Fatalf("promoted capability lost protection: %+v", record.Request)
		}
	}
}
