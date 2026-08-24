// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

type recordingRuntimeSink struct {
	events []RuntimeEvent
	accept bool
	panic  bool
}

func (s *recordingRuntimeSink) TryEmit(event RuntimeEvent) bool {
	if s.panic {
		panic("injected runtime observer panic")
	}
	s.events = append(s.events, event)
	return s.accept
}

func TestRuntimeEventValidatesExactPairs(t *testing.T) {
	valid := []RuntimeEvent{
		{Type: RuntimeTypeStart, Event: RuntimeEventRunStarted},
		{Type: RuntimeTypePhase, Event: RuntimeEventFingerprint},
		{Type: RuntimeTypePhase, Event: RuntimeEventResolution},
		{Type: RuntimeTypePhase, Event: RuntimeEventCollection},
		{Type: RuntimeTypeActivity, Event: RuntimeEventProviderStarted},
		{Type: RuntimeTypeActivity, Event: RuntimeEventProviderFinished},
		{Type: RuntimeTypeActivity, Event: RuntimeEventArtifactSealed},
		{Type: RuntimeTypeHeartbeat, Event: RuntimeEventPulse},
		{Type: RuntimeTypeFinalizing, Event: RuntimeEventStarted},
		{Type: RuntimeTypeVerifying, Event: RuntimeEventStarted},
		{Type: RuntimeTypeTerminal, Event: RuntimeEventRunReturned},
	}
	for _, event := range valid {
		if err := event.Validate(); err != nil {
			t.Fatalf("valid pair %+v rejected: %v", event, err)
		}
	}
	for _, event := range []RuntimeEvent{
		{},
		{Type: RuntimeTypePhase, Event: RuntimeEventRunStarted},
		{Type: RuntimeTypeHeartbeat, Event: RuntimeEventProviderStarted},
		{Type: "UNKNOWN", Event: RuntimeEventPulse},
	} {
		if err := event.Validate(); err == nil {
			t.Fatalf("invalid pair %+v accepted", event)
		}
	}
}

func TestRealFirstStageEmitsHonestRuntimeSequence(t *testing.T) {
	sink := &recordingRuntimeSink{accept: true}
	stage := newRuntimeTestStage(t, sink)
	result, err := stage.Run(context.Background(), runtimeTestRequest(t))
	if err != nil || result.State != RunComplete || !result.FinalizationVerified {
		t.Fatalf("run result=%+v err=%v", result, err)
	}
	want := []RuntimeEvent{
		{Type: RuntimeTypeStart, Event: RuntimeEventRunStarted},
		{Type: RuntimeTypePhase, Event: RuntimeEventFingerprint},
		{Type: RuntimeTypePhase, Event: RuntimeEventResolution},
		{Type: RuntimeTypePhase, Event: RuntimeEventCollection},
		{Type: RuntimeTypeActivity, Event: RuntimeEventProviderStarted},
		{Type: RuntimeTypeActivity, Event: RuntimeEventProviderFinished},
		{Type: RuntimeTypeActivity, Event: RuntimeEventArtifactSealed},
		{Type: RuntimeTypeFinalizing, Event: RuntimeEventStarted},
		{Type: RuntimeTypeVerifying, Event: RuntimeEventStarted},
		{Type: RuntimeTypeTerminal, Event: RuntimeEventRunReturned},
	}
	if !reflect.DeepEqual(sink.events, want) {
		t.Fatalf("runtime events=%+v, want=%+v", sink.events, want)
	}
}

func TestRuntimeSinkNilDropAndPanicDoNotAlterRunResult(t *testing.T) {
	request := runtimeTestRequest(t)
	baseline, err := newRuntimeTestStage(t, nil).Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for name, sink := range map[string]RuntimeEventSink{
		"drop":  &recordingRuntimeSink{accept: false},
		"panic": &recordingRuntimeSink{panic: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, runErr := newRuntimeTestStage(t, sink).Run(context.Background(), request)
			if runErr != nil || !reflect.DeepEqual(got, baseline) {
				t.Fatalf("runtime observer changed result: got=%+v baseline=%+v err=%v", got, baseline, runErr)
			}
		})
	}
}

func TestDiscardedArtifactDoesNotEmitSealedActivity(t *testing.T) {
	sink := &recordingRuntimeSink{accept: true}
	runner := newFakeStreamingRunner()
	runner.result = execution.Result{
		State: execution.Failed, Reason: execution.ReasonProviderError,
		SideEffectSummary: "Fake provider returned without a retainable artifact.",
	}
	stage := newRuntimeTestStageWithRunner(t, runner, sink)
	if _, err := stage.Run(context.Background(), runtimeTestRequest(t)); err != nil {
		t.Fatal(err)
	}
	for _, event := range sink.events {
		if event.Type == RuntimeTypeActivity && event.Event == RuntimeEventArtifactSealed {
			t.Fatal("discarded artifact emitted ARTIFACT_SEALED")
		}
	}
}

func newRuntimeTestStage(t *testing.T, sink RuntimeEventSink) *FirstStage {
	t.Helper()
	return newRuntimeTestStageWithRunner(t, newFakeStreamingRunner(), sink)
}

func newRuntimeTestStageWithRunner(t *testing.T, runner *fakeStreamingRunner, sink RuntimeEventSink) *FirstStage {
	t.Helper()
	session := &fakeFirstStageSession{}
	factory := &fakeFirstStageFactory{session: session}
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: runner,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return testFingerprint(), nil
		},
		PackageFactory: factory.begin,
		Clock:          testNow,
		CollectionID:   func() (string, error) { return testCollectionID, nil },
		RuntimeSink:    sink,
	})
	if err != nil {
		t.Fatalf("construct runtime test FirstStage: %v", err)
	}
	return stage
}

func runtimeTestRequest(t *testing.T) RunRequest {
	t.Helper()
	return RunRequest{CaseID: "CASE-RUNTIME", OutputDestination: filepath.Join(t.TempDir(), "package")}
}
