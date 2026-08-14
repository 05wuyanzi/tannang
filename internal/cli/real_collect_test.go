// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/application"
	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
)

type fakeRealFirstStage struct {
	result  application.RunResult
	err     error
	calls   int
	request application.RunRequest
}

func (f *fakeRealFirstStage) Run(_ context.Context, request application.RunRequest) (application.RunResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

type typedNilRealFirstStage struct{}

func (*typedNilRealFirstStage) Run(context.Context, application.RunRequest) (application.RunResult, error) {
	panic("Run must not be called")
}

type failingTerminalWriter struct {
	buffer bytes.Buffer
	limit  int
	err    error
}

func (w *failingTerminalWriter) Write(value []byte) (int, error) {
	if w.limit <= 0 {
		return 0, w.err
	}
	written := w.limit
	if written > len(value) {
		written = len(value)
	}
	if _, err := w.buffer.Write(value[:written]); err != nil {
		return 0, err
	}
	return written, w.err
}

func (w *failingTerminalWriter) Bytes() []byte {
	return w.buffer.Bytes()
}

func TestRealCollectUsesBoundedSummaryAndExitStates(t *testing.T) {
	tests := []struct {
		name   string
		result application.RunResult
		want   int
	}{
		{"complete", fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone), ExitOK},
		{"partial", fakeRealResult(application.RunPartial, "", execution.Degraded, true, execution.Partial, execution.ReasonNone), ExitPartial},
		{"unavailable", fakeRealResult(application.RunPartial, "", execution.Unavailable, false, execution.Skipped, execution.ReasonAPIUnavailable), ExitSkipped},
		{"blocked", fakeRealResult(application.RunPartial, "", execution.Degraded, true, execution.Blocked, execution.ReasonPrivilegeRequired), ExitBlocked},
		{"provider failed", fakeRealResult(application.RunPartial, "", execution.Available, true, execution.Failed, execution.ReasonProviderError), ExitProviderError},
		{"startup failed", fakeRealResult(application.RunFailed, application.OrchestrationStartupPrerequisiteFailed, execution.Unavailable, false, execution.Skipped, execution.ReasonNone), ExitProviderError},
		{"cancelled", fakeRealResult(application.RunPartial, application.OrchestrationCancelled, execution.Available, false, execution.Skipped, execution.ReasonNone), ExitPartial},
		{"finalization failed", fakeRealResult(application.RunFailed, application.OrchestrationPackageFinalizationFailed, execution.Available, false, execution.Skipped, execution.ReasonNone), ExitIntegrity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stage := &fakeRealFirstStage{result: test.result}
			restore := replaceRealFactory(func() (realFirstStage, error) { return stage, nil })
			defer restore()
			output := filepath.Join(t.TempDir(), "package")
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"collect", "--process-identity-snapshot", "--output", output, "--case-id", "CASE-01"}, &stdout, &stderr)
			if code != test.want || stage.calls != 1 || stage.request.CaseID != "CASE-01" || stage.request.OutputDestination != output {
				t.Fatalf("code=%d calls=%d request=%+v stderr=%s", code, stage.calls, stage.request, stderr.String())
			}
			if strings.Contains(stdout.String(), "provider-detail") || strings.Contains(stdout.String(), "payload-secret") || strings.Contains(stdout.String(), "fingerprint") || strings.Contains(stderr.String(), "provider-detail") {
				t.Fatalf("terminal output leaked bounded data: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			var summary realCollectSummary
			if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil || summary.CollectionID == "" || len(summary.Capabilities) != 1 || summary.Capabilities[0].ID != capability.ProcessIdentitySnapshotID || !summary.Capabilities[0].Protected {
				t.Fatalf("bounded summary=%+v err=%v", summary, err)
			}
		})
	}
}

func TestRealCollectFailsClosedWhenTerminalJSONWriteFails(t *testing.T) {
	writerError := errors.New("SECRET-WRITER-DETAIL")
	for _, test := range []struct {
		name           string
		limit          int
		wantPartialOut bool
	}{
		{name: "total failure", limit: 0},
		{name: "partial failure", limit: 17, wantPartialOut: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stage := &fakeRealFirstStage{result: fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone)}
			restore := replaceRealFactory(func() (realFirstStage, error) { return stage, nil })
			defer restore()
			stdout := &failingTerminalWriter{limit: test.limit, err: writerError}
			var stderr bytes.Buffer
			code := Run(context.Background(), []string{"collect", "--process-identity-snapshot", "--output", filepath.Join(t.TempDir(), "package")}, stdout, &stderr)
			if code != exitTerminalOutput || stage.calls != 1 {
				t.Fatalf("code=%d calls=%d, want code=%d calls=1", code, stage.calls, exitTerminalOutput)
			}
			if got := stderr.String(); got != "failed to write command result\n" || strings.Contains(got, "SECRET-WRITER-DETAIL") {
				t.Fatalf("stderr=%q", got)
			}
			if strings.Contains(string(stdout.Bytes()), "SECRET-WRITER-DETAIL") {
				t.Fatalf("stdout leaked writer error: %q", stdout.Bytes())
			}
			if test.wantPartialOut {
				if len(stdout.Bytes()) == 0 || json.Valid(stdout.Bytes()) {
					t.Fatalf("stdout=%q, want incomplete JSON prefix", stdout.Bytes())
				}
			} else if len(stdout.Bytes()) != 0 {
				t.Fatalf("stdout=%q, want no output", stdout.Bytes())
			}
		})
	}
}

func TestHelpIncludesActivatedBaselineAndExplicitCompatibility(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "collect --output") || !strings.Contains(stdout.String(), "--process-identity-snapshot") || !strings.Contains(stdout.String(), "--synthetic") || strings.Contains(stdout.String(), "broad Windows support") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestNormalCollectActivatesProtectedBaselineWithoutCaseID(t *testing.T) {
	result := fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone)
	result.Context.CaseID = ""
	stage := &fakeRealFirstStage{result: result}
	factoryCalls := 0
	restore := replaceRealFactory(func() (realFirstStage, error) {
		factoryCalls++
		return stage, nil
	})
	defer restore()

	output := filepath.Join(t.TempDir(), "package")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"collect", "--output", output}, &stdout, &stderr)
	if code != ExitOK || factoryCalls != 1 || stage.calls != 1 {
		t.Fatalf("code=%d factory calls=%d stage calls=%d stderr=%s", code, factoryCalls, stage.calls, stderr.String())
	}
	if stage.request.CaseID != "" || stage.request.OutputDestination != output || len(stage.request.Supplemental) != 0 {
		t.Fatalf("request=%+v", stage.request)
	}
	var summary realCollectSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.CaseID != "" || len(summary.Capabilities) != 1 || summary.Capabilities[0].ID != capability.ProcessIdentitySnapshotID || !summary.Capabilities[0].Protected {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestNormalCollectAcceptsOptionalCaseID(t *testing.T) {
	stage := &fakeRealFirstStage{result: fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone)}
	restore := replaceRealFactory(func() (realFirstStage, error) { return stage, nil })
	defer restore()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"collect", "--output", filepath.Join(t.TempDir(), "package"), "--case-id", "CASE-OPTIONAL"}, &stdout, &stderr)
	if code != ExitOK || stage.calls != 1 || stage.request.CaseID != "CASE-OPTIONAL" || len(stage.request.Supplemental) != 0 {
		t.Fatalf("code=%d calls=%d request=%+v stderr=%s", code, stage.calls, stage.request, stderr.String())
	}
}

func TestExplicitProcessFlagConfirmsBaselineWithoutDuplication(t *testing.T) {
	stage := &fakeRealFirstStage{result: fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone)}
	factoryCalls := 0
	restore := replaceRealFactory(func() (realFirstStage, error) {
		factoryCalls++
		return stage, nil
	})
	defer restore()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"collect", "--process-identity-snapshot", "--output", filepath.Join(t.TempDir(), "package")}, &stdout, &stderr)
	if code != ExitOK || factoryCalls != 1 || stage.calls != 1 {
		t.Fatalf("code=%d factory calls=%d stage calls=%d stderr=%s", code, factoryCalls, stage.calls, stderr.String())
	}
	if len(stage.request.Supplemental) != 0 {
		t.Fatalf("explicit flag added supplemental requests: %+v", stage.request.Supplemental)
	}
	var summary realCollectSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil || len(summary.Capabilities) != 1 || summary.Capabilities[0].ID != capability.ProcessIdentitySnapshotID {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestCollectDoesNotExposeProtectedBaselineOptOut(t *testing.T) {
	factoryCalls := 0
	restore := replaceRealFactory(func() (realFirstStage, error) {
		factoryCalls++
		return &fakeRealFirstStage{}, nil
	})
	defer restore()

	for _, option := range []string{
		"--no-process-identity-snapshot",
		"--skip-baseline",
		"--disable-baseline",
		"--no-baseline",
	} {
		t.Run(option, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"collect", option, "--output", filepath.Join(t.TempDir(), "package")}, &stdout, &stderr)
			if code != ExitUsage {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
		})
	}
	if factoryCalls != 0 {
		t.Fatalf("factory calls=%d, want 0", factoryCalls)
	}
}

func TestSyntheticCollectDoesNotConstructRealFirstStage(t *testing.T) {
	factoryCalls := 0
	restore := replaceRealFactory(func() (realFirstStage, error) {
		factoryCalls++
		return &fakeRealFirstStage{}, nil
	})
	defer restore()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"collect", "--synthetic", "available-collected", "--output", filepath.Join(t.TempDir(), "package")}, &stdout, &stderr)
	if code != ExitOK || factoryCalls != 0 {
		t.Fatalf("code=%d factory calls=%d stderr=%s", code, factoryCalls, stderr.String())
	}
}

func TestRealCollectRejectsInvalidModesAndRequestsBeforeFactory(t *testing.T) {
	calls := 0
	restore := replaceRealFactory(func() (realFirstStage, error) { calls++; return &fakeRealFirstStage{}, nil })
	defer restore()
	output := filepath.Join(t.TempDir(), "package")
	for name, args := range map[string][]string{
		"unexpected argument":              {"collect", "--output", output, "extra"},
		"conflicting modes":                {"collect", "--synthetic", "available-collected", "--process-identity-snapshot", "--output", output},
		"conflicting false real flag":      {"collect", "--synthetic", "available-collected", "--process-identity-snapshot=false", "--output", output},
		"empty synthetic fixture":          {"collect", "--synthetic", "", "--output", output},
		"missing output":                   {"collect", "--process-identity-snapshot"},
		"case on synthetic":                {"collect", "--synthetic", "available-collected", "--case-id", "CASE-01", "--output", output},
		"empty case flag on synthetic":     {"collect", "--synthetic", "available-collected", "--case-id", "", "--output", output},
		"invalid case":                     {"collect", "--case-id", "\x01", "--output", output},
		"explicit false baseline selector": {"collect", "--process-identity-snapshot=false", "--output", output},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr); code != ExitUsage {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
		})
	}
	if calls != 0 {
		t.Fatalf("factory calls=%d, want 0", calls)
	}
}

func TestRealCollectFailsClosedBeforeAcquisition(t *testing.T) {
	t.Run("factory error", func(t *testing.T) {
		restore := replaceRealFactory(func() (realFirstStage, error) { return nil, errors.New("injected") })
		defer restore()
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"collect", "--process-identity-snapshot", "--output", filepath.Join(t.TempDir(), "package")}, &stdout, &stderr); code != ExitProviderError {
			t.Fatalf("code=%d", code)
		}
	})
	t.Run("typed nil", func(t *testing.T) {
		var stage *typedNilRealFirstStage
		restore := replaceRealFactory(func() (realFirstStage, error) { return stage, nil })
		defer restore()
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"collect", "--process-identity-snapshot", "--output", filepath.Join(t.TempDir(), "package")}, &stdout, &stderr); code != ExitProviderError {
			t.Fatalf("code=%d", code)
		}
	})
	t.Run("nil factory", func(t *testing.T) {
		previous := newRealFirstStage
		newRealFirstStage = nil
		defer func() { newRealFirstStage = previous }()
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"collect", "--process-identity-snapshot", "--output", filepath.Join(t.TempDir(), "package")}, &stdout, &stderr); code != ExitProviderError {
			t.Fatalf("code=%d", code)
		}
	})
	for name, output := range map[string]string{
		"relative output":  "relative-package",
		"traversal output": filepath.Join("..", "package"),
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			restore := replaceRealFactory(func() (realFirstStage, error) { calls++; return &fakeRealFirstStage{}, nil })
			defer restore()
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), []string{"collect", "--output", output}, &stdout, &stderr); code != ExitPathSafety || calls != 0 {
				t.Fatalf("code=%d calls=%d", code, calls)
			}
		})
	}
}

func fakeRealResult(state application.RunState, orchestration application.OrchestrationReason, compatibility execution.CompatibilityState, attempted bool, executionState execution.State, reason execution.Reason) application.RunResult {
	request := capability.CapabilityRequest{ID: capability.ProcessIdentitySnapshotID, Priority: capability.PriorityNormal, Protected: true}
	record := application.CapabilityRecord{
		Request: request, Compatibility: compatibility, Attempted: attempted,
		Execution:       execution.Result{State: executionState, Reason: reason, Detail: "provider-detail", SideEffectSummary: "bounded test result", Payload: json.RawMessage(`{"secret":"payload-secret"}`)},
		MissingEvidence: []string{"Evidence was not fully collected."}, ReceiptReference: "receipts/PROCESS_IDENTITY_SNAPSHOT.json",
	}
	if executionState == execution.Collected {
		record.MissingEvidence = nil
		record.ArtifactReference = "derived/process-identity-snapshot.ndjson"
	}
	if orchestration == application.OrchestrationCancelled || orchestration == application.OrchestrationPackageFinalizationFailed {
		record.Attempted = false
		record.Execution = execution.Result{State: execution.Skipped, Reason: execution.ReasonNone, SideEffectSummary: "No provider was executed."}
		record.OrchestrationReason = orchestration
	}
	return application.RunResult{
		Context: application.CollectionContext{CollectionID: "COL-00000000-0000-4000-8000-000000000001", CaseID: "CASE-01", OutputDestination: `C:\test\package`, StartedAt: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
		State:   state, OrchestrationReason: orchestration, Records: []application.CapabilityRecord{record},
		FinalizationVerified: state != application.RunFailed, PackageReference: `C:\test\package`,
	}
}

func replaceRealFactory(factory realFirstStageFactory) func() {
	previous := newRealFirstStage
	newRealFirstStage = factory
	return func() { newRealFirstStage = previous }
}
