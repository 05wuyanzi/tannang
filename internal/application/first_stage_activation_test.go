// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
)

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
