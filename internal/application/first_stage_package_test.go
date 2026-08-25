// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/buildinfo"
	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

type fakeStreamingRunner struct {
	descriptor provider.Descriptor
	result     execution.Result
	artifact   []byte
	execute    func(context.Context, io.Writer) execution.Result
	calls      int
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected writer failure")
}

func (f *fakeStreamingRunner) Descriptor() provider.Descriptor { return cloneDescriptor(f.descriptor) }
func (f *fakeStreamingRunner) Artifact() provider.ArtifactDescriptor {
	return provider.ArtifactDescriptor{MediaType: receipt.FirstStageArtifactMedia, ContentSchemaID: receipt.FirstStageArtifactSchema}
}
func (f *fakeStreamingRunner) ExecuteTo(ctx context.Context, _ capability.Capability, _ fingerprint.TargetFingerprint, writer io.Writer) execution.Result {
	f.calls++
	if f.execute != nil {
		return f.execute(ctx, writer)
	}
	if len(f.artifact) > 0 {
		if _, err := writer.Write(f.artifact); err != nil {
			return execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, Detail: err.Error(), SideEffectSummary: "Fake writer failed."}
		}
	}
	return cloneExecutionResult(f.result)
}

type fakeFirstStageSession struct {
	buffer        bytes.Buffer
	openCalls     int
	sealCalls     int
	retain        bool
	hashCalls     int
	finalizeCalls int
	abortCalls    int
	openErr       error
	sealErr       error
	hashErr       error
	finalizeErr   error
	abortErr      error
	metadata      receipt.FirstStagePackageMetadata
	receipts      []receipt.FirstStageRecord
	writer        io.Writer
	finalize      func(context.Context, receipt.FirstStagePackageMetadata, []receipt.FirstStageRecord) error
	onOpen        func()
	onSeal        func(bool)
	mu            sync.Mutex
}

func (f *fakeFirstStageSession) OpenArtifact() (io.Writer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCalls++
	if f.onOpen != nil {
		f.onOpen()
	}
	if f.writer != nil {
		return f.writer, f.openErr
	}
	return &f.buffer, f.openErr
}
func (f *fakeFirstStageSession) SealArtifact(retain bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sealCalls++
	f.retain = retain
	if f.onSeal != nil {
		f.onSeal(retain)
	}
	return f.sealErr
}
func (f *fakeFirstStageSession) HashArtifact() (integrity.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hashCalls++
	if f.hashErr != nil {
		return integrity.Entry{}, f.hashErr
	}
	digest := sha256.Sum256(f.buffer.Bytes())
	return integrity.Entry{Path: receipt.FirstStageArtifactPath, Size: int64(f.buffer.Len()), SHA256: hex.EncodeToString(digest[:])}, nil
}
func (f *fakeFirstStageSession) Finalize(ctx context.Context, metadata receipt.FirstStagePackageMetadata, records []receipt.FirstStageRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finalizeCalls++
	f.metadata = metadata
	f.receipts = append([]receipt.FirstStageRecord(nil), records...)
	if f.finalize != nil {
		return f.finalize(ctx, metadata, records)
	}
	return f.finalizeErr
}
func (f *fakeFirstStageSession) Abort() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abortCalls++
	return f.abortErr
}

type fakeFirstStageFactory struct {
	session *fakeFirstStageSession
	err     error
	calls   int
	onBegin func()
}

func (f *fakeFirstStageFactory) begin(_ context.Context, _ string) (firstStagePackageSession, error) {
	f.calls++
	if f.onBegin != nil {
		f.onBegin()
	}
	return f.session, f.err
}

type fakeMultiArtifactSession struct {
	fakeFirstStageSession
	streamOpenCalls int
	reserveCalls    int
	discardCalls    int
	processRetained bool
	eventReserved   bool
	eventSealed     bool
	eventSealErr    error
	stagingPath     string
	onReserve       func()
}

func (f *fakeMultiArtifactSession) OpenStreamingArtifact(capabilityID string) (io.Writer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if capabilityID != capability.ProcessIdentitySnapshotID {
		return nil, errors.New("unexpected streaming artifact capability")
	}
	f.streamOpenCalls++
	return &f.buffer, nil
}

func (f *fakeMultiArtifactSession) ReserveFileArtifactPath(capabilityID string) (string, error) {
	f.mu.Lock()
	if capabilityID != capability.WindowsEventLogSystemChannelID {
		f.mu.Unlock()
		return "", errors.New("unexpected file artifact capability")
	}
	f.reserveCalls++
	f.eventReserved = true
	stagingPath := f.stagingPath
	onReserve := f.onReserve
	f.mu.Unlock()
	if onReserve != nil {
		onReserve()
	}
	return stagingPath, nil
}

func (f *fakeMultiArtifactSession) ValidateFileArtifact(capabilityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if capabilityID != capability.WindowsEventLogSystemChannelID || !f.eventReserved {
		return errors.New("file artifact was not reserved")
	}
	return nil
}

func (f *fakeMultiArtifactSession) SealNamedArtifact(capabilityID string, retain bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch capabilityID {
	case capability.ProcessIdentitySnapshotID:
		f.processRetained = retain
		return nil
	case capability.WindowsEventLogSystemChannelID:
		if !f.eventReserved {
			return errors.New("file artifact was not reserved")
		}
		f.discardCalls++
		f.eventSealed = true
		if retain {
			return errors.New("test Event Log artifact must not be retained in this seam")
		}
		return f.eventSealErr
	default:
		return errors.New("unexpected named artifact capability")
	}
}

func (f *fakeMultiArtifactSession) HashNamedArtifact(capabilityID string) (integrity.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if capabilityID != capability.ProcessIdentitySnapshotID || !f.processRetained {
		return integrity.Entry{}, errors.New("process artifact was not retained")
	}
	digest := sha256.Sum256(f.buffer.Bytes())
	return integrity.Entry{Path: receipt.FirstStageArtifactPath, Size: int64(f.buffer.Len()), SHA256: hex.EncodeToString(digest[:])}, nil
}

type fakeMultiArtifactFactory struct {
	session *fakeMultiArtifactSession
	calls   int
}

func (f *fakeMultiArtifactFactory) begin(_ context.Context, _ string) (firstStagePackageSession, error) {
	f.calls++
	return f.session, nil
}

type cancellationFileArtifactRunner struct {
	calls int
}

func (r *cancellationFileArtifactRunner) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:           provider.WindowsEventLogSystemProviderID,
		Class:        provider.FirstPartyNative,
		Capabilities: []string{capability.WindowsEventLogSystemChannelID},
		Requirements: provider.Requirements{Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"}, Architectures: []string{"amd64"}, Available: true, AvailabilityReason: execution.ReasonNone},
		Quality:      provider.Quality{Compatibility: execution.Available, Reason: execution.ReasonNone, Fidelity: 5, Disturbance: 1, Completeness: 5, OutputStability: 5, EvidenceValue: 5},
	}
}

func (r *cancellationFileArtifactRunner) Artifact() provider.ArtifactDescriptor {
	return provider.ArtifactDescriptor{MediaType: receipt.WindowsEventLogSystemMediaType, ContentSchemaID: receipt.WindowsEventLogSystemSchemaID}
}

func (r *cancellationFileArtifactRunner) ExecuteToPath(context.Context, capability.Capability, fingerprint.TargetFingerprint, string) execution.Result {
	r.calls++
	return execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "test Event Log Provider executed"}
}

func newRealEventLogTestStage(t *testing.T, process provider.StreamingRunner, event provider.FileArtifactRunner, factory firstStagePackageFactory) *FirstStage {
	t.Helper()
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: process,
		EventLogRunner:  event,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return testFingerprint(), nil
		},
		PackageFactory: factory,
		Clock:          testNow,
		CollectionID:   func() (string, error) { return testCollectionID, nil },
	})
	if err != nil {
		t.Fatalf("construct real Event Log test stage: %v", err)
	}
	return stage
}

func TestRealFirstStageEventLogPreExecutionCancellationDiscardsReservedArtifact(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	process := newFakeStreamingRunner()
	event := &cancellationFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "windows-event-log-system.evtx")}
	session.onReserve = cancel
	factory := &fakeMultiArtifactFactory{session: session}
	stage := newRealEventLogTestStage(t, process, event, factory.begin)
	request := realRunRequest(t)
	request.Supplemental = []capability.CapabilityRequest{{ID: capability.WindowsEventLogSystemChannelID, Priority: capability.PriorityLate}}

	result, err := stage.Run(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || result.OrchestrationReason != OrchestrationCancelled || !result.FinalizationVerified || result.PackageReference == "" {
		t.Fatalf("pre-execution cancellation result = %+v", result)
	}
	if factory.calls != 1 || session.streamOpenCalls != 1 || session.reserveCalls != 1 || session.discardCalls != 1 || !session.eventSealed || !session.processRetained || session.finalizeCalls != 1 || session.abortCalls != 0 {
		t.Fatalf("pre-execution cancellation lifecycle = factory=%d stream=%d reserve=%d discard=%d sealed=%v processRetained=%v finalize=%d abort=%d", factory.calls, session.streamOpenCalls, session.reserveCalls, session.discardCalls, session.eventSealed, session.processRetained, session.finalizeCalls, session.abortCalls)
	}
	if event.calls != 0 {
		t.Fatalf("Event Log Provider calls = %d, want 0 after pre-execution cancellation", event.calls)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want process plus Event Log", len(result.Records))
	}
	processRecord, eventRecord := result.Records[0], result.Records[1]
	if processRecord.Request.ID != capability.ProcessIdentitySnapshotID || !processRecord.Attempted || processRecord.Execution.State != execution.Collected || processRecord.ArtifactReference == "" {
		t.Fatalf("process evidence was not retained: %+v", processRecord)
	}
	if eventRecord.Request.ID != capability.WindowsEventLogSystemChannelID || eventRecord.Attempted || eventRecord.Execution.State != execution.Skipped || eventRecord.Execution.Reason != execution.ReasonNone || eventRecord.OrchestrationReason != OrchestrationCancelled || eventRecord.ArtifactReference != "" {
		t.Fatalf("Event Log cancellation accounting = %+v", eventRecord)
	}
}

func TestRealFirstStageEventLogPreExecutionCancellationDiscardFailureFailsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	process := newFakeStreamingRunner()
	event := &cancellationFileArtifactRunner{}
	session := &fakeMultiArtifactSession{
		stagingPath:  filepath.Join(t.TempDir(), "windows-event-log-system.evtx"),
		eventSealErr: errors.New("injected Event Log discard failure"),
	}
	session.onReserve = cancel
	factory := &fakeMultiArtifactFactory{session: session}
	stage := newRealEventLogTestStage(t, process, event, factory.begin)
	request := realRunRequest(t)
	request.Supplemental = []capability.CapabilityRequest{{ID: capability.WindowsEventLogSystemChannelID, Priority: capability.PriorityLate}}

	result, err := stage.Run(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || result.FinalizationVerified || result.PackageReference != "" {
		t.Fatalf("discard failure result = %+v", result)
	}
	if event.calls != 0 || session.reserveCalls != 1 || session.discardCalls != 1 || session.finalizeCalls != 0 || session.abortCalls != 1 {
		t.Fatalf("discard failure lifecycle = event=%d reserve=%d discard=%d finalize=%d abort=%d", event.calls, session.reserveCalls, session.discardCalls, session.finalizeCalls, session.abortCalls)
	}
	if len(result.Records) != 2 || result.Records[1].ArtifactReference != "" || result.Records[1].Attempted || result.Records[1].Execution.State != execution.Skipped || result.Records[1].Execution.Reason != execution.ReasonNone || result.Records[1].OrchestrationReason != OrchestrationCancelled {
		t.Fatalf("discard failure fabricated Event Log evidence: %+v", result.Records)
	}
}

func TestRealFirstStageSelectedLifecycleUsesOneSession(t *testing.T) {
	runner := newFakeStreamingRunner()
	session := &fakeFirstStageSession{}
	factory := &fakeFirstStageFactory{session: session}
	stage := newRealTestStage(t, runner, factory.begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || !result.FinalizationVerified || result.PackageReference == "" {
		t.Fatalf("unexpected real result: %+v", result)
	}
	if factory.calls != 1 || session.openCalls != 1 || runner.calls != 1 || session.finalizeCalls != 1 || session.abortCalls != 0 {
		t.Fatalf("lifecycle counts begin=%d open=%d execute=%d finalize=%d abort=%d", factory.calls, session.openCalls, runner.calls, session.finalizeCalls, session.abortCalls)
	}
	if !session.retain || session.hashCalls != 1 || len(session.receipts) != 1 || len(session.metadata.ArtifactReferences) != 1 {
		t.Fatalf("retained package facts are incomplete: session=%+v", session)
	}
	if session.metadata.StartedAt == "" || session.metadata.FinishedAt == "" || session.receipts[0].AcquisitionStartedAt != session.metadata.StartedAt || session.receipts[0].AcquisitionFinishedAt != session.metadata.FinishedAt {
		t.Fatalf("package and receipt timestamps are inconsistent: metadata=%+v receipt=%+v", session.metadata, session.receipts[0])
	}
	productVersion := buildinfo.Current().ProductVersion
	if session.metadata.ProductVersion != productVersion {
		t.Fatalf("metadata product version = %q, want %q", session.metadata.ProductVersion, productVersion)
	}
	for index, record := range session.receipts {
		if record.ProductVersion != session.metadata.ProductVersion {
			t.Fatalf("receipt %d product version = %q, metadata = %q", index, record.ProductVersion, session.metadata.ProductVersion)
		}
	}
}

func TestRealFirstStageFakeProviderPublishesVerifiedPackage(t *testing.T) {
	for name, result := range map[string]execution.Result{
		"collected": {State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Fake collection completed."},
		"partial":   {State: execution.Partial, Reason: execution.ReasonAPIUnavailable, SideEffectSummary: "Fake collection returned a complete-row prefix."},
		"failed":    {State: execution.Failed, Reason: execution.ReasonProviderError, SideEffectSummary: "Fake collection failed."},
		"blocked":   {State: execution.Blocked, Reason: execution.ReasonPrivilegeRequired, SideEffectSummary: "Fake collection was blocked."},
		"skipped":   {State: execution.Skipped, Reason: execution.ReasonPolicyDisabled, SideEffectSummary: "Fake collection was skipped."},
	} {
		name, result := name, result
		t.Run(name, func(t *testing.T) {
			runner := newFakeStreamingRunner()
			runner.result = result
			stage := newRealTestStage(t, runner, defaultFirstStagePackageFactory)
			request := realRunRequest(t)
			runResult, err := stage.Run(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if err := integrity.Verify(request.OutputDestination); err != nil {
				t.Fatalf("verify fake-provider package: %v", err)
			}
			artifactPath := filepath.Join(request.OutputDestination, filepath.FromSlash(receipt.FirstStageArtifactPath))
			_, artifactErr := os.Stat(artifactPath)
			retainable := result.State == execution.Collected || result.State == execution.Partial
			if retainable && artifactErr != nil {
				t.Fatalf("retainable artifact missing: %v", artifactErr)
			}
			if !retainable && !os.IsNotExist(artifactErr) {
				t.Fatalf("non-retainable artifact remained: %v", artifactErr)
			}
			wantState := RunPartial
			if result.State == execution.Collected {
				wantState = RunComplete
			}
			if runResult.State != wantState || !runResult.FinalizationVerified {
				t.Fatalf("run state=%s verified=%v want=%s", runResult.State, runResult.FinalizationVerified, wantState)
			}
		})
	}
}

func TestRealFirstStageWriterFailureDiscardsWholeSink(t *testing.T) {
	runner := newFakeStreamingRunner()
	session := &fakeFirstStageSession{writer: failingWriter{}}
	factory := &fakeFirstStageFactory{session: session}
	stage := newRealTestStage(t, runner, factory.begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if result.State != RunPartial || !result.FinalizationVerified || !record.Attempted || record.Execution.State != execution.Failed || record.Execution.Reason != execution.ReasonProviderError || record.ArtifactReference != "" {
		t.Fatalf("writer failure accounting = result=%+v record=%+v", result, record)
	}
	if session.sealCalls != 1 || session.retain || session.hashCalls != 0 || session.finalizeCalls != 1 {
		t.Fatalf("writer failure lifecycle = %+v", session)
	}
}

func TestRealFirstStagePackageFailureClassesPreserveProviderFacts(t *testing.T) {
	for _, name := range []string{
		"sync", "close", "remove", "hash", "receipt", "metadata", "manifest generate", "manifest verify", "publish",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			runner := newFakeStreamingRunner()
			session := &fakeFirstStageSession{}
			expectedState := execution.Collected
			expectedReason := execution.ReasonNone
			switch name {
			case "sync", "close":
				session.sealErr = errors.New("injected " + name + " failure")
			case "remove":
				runner.result = execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, SideEffectSummary: "Fake Provider failed before discard."}
				expectedState = execution.Failed
				expectedReason = execution.ReasonProviderError
				session.sealErr = errors.New("injected remove failure")
			case "hash":
				session.hashErr = errors.New("injected hash failure")
			default:
				session.finalizeErr = errors.New("injected " + name + " failure")
			}
			factory := &fakeFirstStageFactory{session: session}
			stage := newRealTestStage(t, runner, factory.begin)
			result, err := stage.Run(context.Background(), realRunRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			record := result.Records[0]
			if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || result.FinalizationVerified || result.PackageReference != "" {
				t.Fatalf("package failure result = %+v", result)
			}
			if !record.Attempted || record.Execution.State != expectedState || record.Execution.Reason != expectedReason || record.ArtifactReference != "" || record.ReceiptReference != "" {
				t.Fatalf("provider facts were rewritten for %s: %+v", name, record)
			}
			if session.abortCalls != 1 {
				t.Fatalf("%s failure Abort calls = %d, want 1", name, session.abortCalls)
			}
		})
	}
}

func TestRealFirstStageFinalizationTimeoutUsesPackageFailure(t *testing.T) {
	runner := newFakeStreamingRunner()
	session := &fakeFirstStageSession{}
	session.finalize = func(ctx context.Context, _ receipt.FirstStagePackageMetadata, _ []receipt.FirstStageRecord) error {
		<-ctx.Done()
		return ctx.Err()
	}
	factory := &fakeFirstStageFactory{session: session}
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Millisecond, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: runner,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return testFingerprint(), nil
		},
		PackageFactory: factory.begin,
		Clock:          testNow,
		CollectionID:   func() (string, error) { return testCollectionID, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || session.abortCalls != 1 {
		t.Fatalf("finalization timeout accounting = result=%+v abort=%d", result, session.abortCalls)
	}
}

func TestRealFirstStagePreAttemptPackageFailures(t *testing.T) {
	for name, configure := range map[string]func(*fakeFirstStageFactory, *fakeFirstStageSession){
		"begin": func(factory *fakeFirstStageFactory, _ *fakeFirstStageSession) {
			factory.err = errors.New("begin failed")
		},
		"open": func(_ *fakeFirstStageFactory, session *fakeFirstStageSession) {
			session.openErr = errors.New("open failed")
		},
		"nil session": func(factory *fakeFirstStageFactory, _ *fakeFirstStageSession) {
			factory.session = nil
		},
	} {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			runner := newFakeStreamingRunner()
			session := &fakeFirstStageSession{}
			factory := &fakeFirstStageFactory{session: session}
			configure(factory, session)
			stage := newRealTestStage(t, runner, factory.begin)
			request := realRunRequest(t)
			request.Supplemental = []capability.CapabilityRequest{{ID: "UNKNOWN_LATE", Priority: capability.PriorityLate}}
			result, err := stage.Run(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			record := result.Records[0]
			if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || record.Attempted || record.Execution.State != execution.Skipped || record.Execution.Reason != execution.ReasonNone || record.SelectedProvider == nil || record.Decision == nil || len(record.MissingEvidence) != 1 {
				t.Fatalf("pre-attempt package failure accounting = %+v / %+v", result, record)
			}
			if err := record.Validate(); err != nil {
				t.Fatalf("pre-attempt record did not validate: %v", err)
			}
			wantAbort := 1
			if name == "nil session" {
				wantAbort = 0
			}
			if runner.calls != 0 || session.abortCalls != wantAbort || record.ArtifactReference != "" || record.ReceiptReference != "" {
				t.Fatalf("pre-attempt lifecycle execute=%d abort=%d record=%+v", runner.calls, session.abortCalls, record)
			}
			for _, terminal := range result.Records {
				if err := terminal.Validate(); err != nil {
					t.Fatalf("terminal record %q did not validate after package failure: %v", terminal.Request.ID, err)
				}
			}
		})
	}
}

func TestRealFirstStageReceiptOnlyNilSessionFailsClosed(t *testing.T) {
	runner := newFakeStreamingRunner()
	runner.descriptor.Requirements.Available = false
	runner.descriptor.Requirements.AvailabilityReason = execution.ReasonAPIUnavailable
	factory := &fakeFirstStageFactory{}
	stage := newRealTestStage(t, runner, factory.begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || factory.calls != 1 {
		t.Fatalf("nil receipt-only session did not fail closed: result=%+v begin=%d", result, factory.calls)
	}
}

func TestRealFirstStageUnavailablePublishesReceiptOnlyPackage(t *testing.T) {
	runner := newFakeStreamingRunner()
	runner.descriptor.Requirements.Available = false
	runner.descriptor.Requirements.AvailabilityReason = execution.ReasonAPIUnavailable
	stage := newRealTestStage(t, runner, defaultFirstStagePackageFactory)
	request := realRunRequest(t)
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if result.State != RunPartial || !result.FinalizationVerified || record.Attempted || record.Execution.State != execution.Skipped || record.Execution.Reason != execution.ReasonAPIUnavailable || record.ArtifactReference != "" || record.ReceiptReference == "" {
		t.Fatalf("unavailable receipt-only result = %+v record=%+v", result, record)
	}
	if err := integrity.Verify(request.OutputDestination); err != nil {
		t.Fatalf("unavailable receipt-only package failed verification: %v", err)
	}
	metadataBytes, err := pathsafe.ReadFile(request.OutputDestination, "meta/package.json")
	if err != nil {
		t.Fatal(err)
	}
	compactMetadata := bytes.ReplaceAll(metadataBytes, []byte(" "), nil)
	compactMetadata = bytes.ReplaceAll(compactMetadata, []byte("\n"), nil)
	if bytes.Contains(compactMetadata, []byte(`"artifact_references":null`)) || !bytes.Contains(compactMetadata, []byte(`"artifact_references":[]`)) {
		t.Fatalf("receipt-only metadata did not encode an empty artifact array: %s", metadataBytes)
	}
}

func TestRealFirstStageCancellationSessionBoundaries(t *testing.T) {
	t.Run("before begin", func(t *testing.T) {
		runner := newFakeStreamingRunner()
		session := &fakeFirstStageSession{}
		factory := &fakeFirstStageFactory{session: session}
		stage := newRealTestStage(t, runner, factory.begin)
		ctx := &countingCancelContext{cancelAt: 9}
		result, err := stage.Run(ctx, realRunRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		if factory.calls != 1 || session.openCalls != 0 || runner.calls != 0 || session.finalizeCalls != 1 || result.State != RunPartial {
			t.Fatalf("before-begin lifecycle result=%+v begin=%d open=%d execute=%d finalize=%d", result, factory.calls, session.openCalls, runner.calls, session.finalizeCalls)
		}
	})

	t.Run("after begin before open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := newFakeStreamingRunner()
		session := &fakeFirstStageSession{}
		factory := &fakeFirstStageFactory{session: session, onBegin: cancel}
		stage := newRealTestStage(t, runner, factory.begin)
		result, err := stage.Run(ctx, realRunRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		if factory.calls != 1 || session.openCalls != 0 || runner.calls != 0 || session.finalizeCalls != 1 || result.Records[0].Attempted {
			t.Fatalf("after-begin lifecycle result=%+v", result)
		}
	})

	t.Run("after open before execute", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := newFakeStreamingRunner()
		session := &fakeFirstStageSession{onOpen: cancel}
		factory := &fakeFirstStageFactory{session: session}
		stage := newRealTestStage(t, runner, factory.begin)
		result, err := stage.Run(ctx, realRunRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		if factory.calls != 1 || session.openCalls != 1 || session.sealCalls != 1 || session.retain || runner.calls != 0 || session.finalizeCalls != 1 || result.Records[0].Attempted {
			t.Fatalf("after-open lifecycle result=%+v session=%+v", result, session)
		}
	})

	t.Run("after open cleanup failure preserves cancellation facts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := newFakeStreamingRunner()
		session := &fakeFirstStageSession{onOpen: cancel, sealErr: errors.New("injected discard failure")}
		factory := &fakeFirstStageFactory{session: session}
		stage := newRealTestStage(t, runner, factory.begin)
		result, err := stage.Run(ctx, realRunRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		record := result.Records[0]
		if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || record.OrchestrationReason != OrchestrationCancelled || record.Attempted || record.Execution.State != execution.Skipped || record.Execution.Reason != execution.ReasonNone || len(record.MissingEvidence) == 0 || record.ReceiptReference != "" || record.ArtifactReference != "" || session.abortCalls != 1 {
			t.Fatalf("cleanup failure rewrote cancellation facts: result=%+v record=%+v session=%+v", result, record, session)
		}
		if err := record.Validate(); err != nil {
			t.Fatalf("cancelled record invalid after cleanup failure: %v", err)
		}
	})
}

func TestRealFirstStageCancellationFactsPrecedeArtifactCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := newFakeStreamingRunner()
	var record CapabilityRecord
	var observedErr error
	session := &fakeFirstStageSession{
		onOpen:  cancel,
		sealErr: errors.New("injected discard failure"),
		onSeal: func(bool) {
			if record.OrchestrationReason != OrchestrationCancelled || record.Attempted || record.Execution.State != execution.Skipped || record.Execution.Reason != execution.ReasonNone {
				observedErr = errors.New("cancellation facts were not terminal before artifact cleanup")
				return
			}
			observedErr = record.Validate()
		},
	}
	factory := &fakeFirstStageFactory{session: session}
	stage := newRealTestStage(t, runner, factory.begin)
	record = stage.skeletonRecords(stage.baseline)[0]
	decision, err := stage.resolve(*record.Capability, testFingerprint(), cloneDescriptors(stage.descriptors), stage.resolverPolicy)
	if err != nil || decision.Selected == nil {
		t.Fatalf("prepare selected record: decision=%+v err=%v", decision, err)
	}
	record.Decision = &decision
	record.Compatibility = decision.Compatibility
	selected := cloneDescriptor(*decision.Selected)
	record.SelectedProvider = &selected
	var owned firstStagePackageSession
	cancelled := false
	err = stage.runRealSelected(ctx, CollectionContext{OutputDestination: t.TempDir()}, testFingerprint(), &record, &owned, &cancelled)
	if err == nil || observedErr != nil || !cancelled || session.abortCalls != 1 {
		t.Fatalf("cleanup ordering err=%v observed=%v cancelled=%v abort=%d record=%+v", err, observedErr, cancelled, session.abortCalls, record)
	}
}

func TestRealFirstStageUnavailableReceiptOnlyFailurePreservesFacts(t *testing.T) {
	runner := newFakeStreamingRunner()
	runner.descriptor.Requirements.Available = false
	runner.descriptor.Requirements.AvailabilityReason = execution.ReasonAPIUnavailable
	session := &fakeFirstStageSession{}
	factory := &fakeFirstStageFactory{session: session, err: errors.New("receipt begin failed")}
	stage := newRealTestStage(t, runner, factory.begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	record := result.Records[0]
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed || record.SelectedProvider != nil || record.Attempted || record.Compatibility != execution.Unavailable || record.Execution.State != execution.Skipped || session.openCalls != 0 || session.abortCalls != 1 {
		t.Fatalf("receipt-only failure changed capability facts: result=%+v record=%+v", result, record)
	}
}

type countingCancelContext struct {
	mu       sync.Mutex
	calls    int
	cancelAt int
}

func (c *countingCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *countingCancelContext) Done() <-chan struct{}       { return nil }
func (c *countingCancelContext) Value(any) any               { return nil }
func (c *countingCancelContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func newRealTestStage(t *testing.T, runner provider.StreamingRunner, factory firstStagePackageFactory) *FirstStage {
	t.Helper()
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: runner,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) {
			return testFingerprint(), nil
		},
		PackageFactory: factory,
		Clock:          testNow,
		CollectionID:   func() (string, error) { return testCollectionID, nil },
	})
	if err != nil {
		t.Fatalf("construct real test FirstStage: %v", err)
	}
	return stage
}

func newFakeStreamingRunner() *fakeStreamingRunner {
	return &fakeStreamingRunner{
		descriptor: provider.Descriptor{
			ID: processIdentitySnapshotProviderID, Class: provider.FirstPartyNative,
			Capabilities: []string{capability.ProcessIdentitySnapshotID},
			Requirements: provider.Requirements{
				Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"}, Architectures: []string{"amd64"},
				Available: true, AvailabilityReason: execution.ReasonNone,
			},
			Quality: provider.Quality{Compatibility: execution.Available, Reason: execution.ReasonNone, Fidelity: 4, Completeness: 2, OutputStability: 4, EvidenceValue: 3},
		},
		artifact: []byte("{\"process_id\":1,\"parent_process_id\":0,\"executable_name\":\"fixture.exe\"}\n"),
		result:   execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Fake process snapshot completed."},
	}
}

func realRunRequest(t *testing.T) RunRequest {
	t.Helper()
	return RunRequest{CaseID: "CASE-REAL-TEST", OutputDestination: filepath.Join(t.TempDir(), "first-stage-package")}
}
