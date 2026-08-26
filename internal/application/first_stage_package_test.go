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
	descriptor         provider.Descriptor
	artifactDescriptor provider.ArtifactDescriptor
	result             execution.Result
	artifact           []byte
	execute            func(context.Context, io.Writer) execution.Result
	calls              int
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected writer failure")
}

func (f *fakeStreamingRunner) Descriptor() provider.Descriptor { return cloneDescriptor(f.descriptor) }
func (f *fakeStreamingRunner) Artifact() provider.ArtifactDescriptor {
	if f.artifactDescriptor.MediaType != "" {
		return f.artifactDescriptor
	}
	return provider.ArtifactDescriptor{MediaType: receipt.FirstStageArtifactMedia, ContentSchemaID: receipt.FirstStageArtifactSchema}
}
func (f *fakeStreamingRunner) Probe(context.Context, fingerprint.TargetFingerprint) (execution.Reason, error) {
	if f.descriptor.Requirements.Available {
		return execution.ReasonNone, nil
	}
	return f.descriptor.Requirements.AvailabilityReason, nil
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
	streamOpenCalls   int
	reserveCalls      int
	discardCalls      int
	processRetained   bool
	hostRetained      bool
	transportRetained bool
	hostFailed        bool
	eventReserved     bool
	eventSealed       bool
	eventRetained     bool
	allowEventRetain  bool
	eventSealErr      error
	stagingPath       string
	onReserve         func()
}

func (f *fakeMultiArtifactSession) OpenStreamingArtifact(capabilityID string) (io.Writer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if capabilityID != capability.ProcessIdentitySnapshotID && capabilityID != capability.WindowsHostOSIdentitySnapshotID && capabilityID != capability.WindowsTransportEndpointSnapshotID {
		return nil, errors.New("unexpected streaming artifact capability")
	}
	f.streamOpenCalls++
	if capabilityID == capability.WindowsHostOSIdentitySnapshotID {
		f.hostFailed = false
	}
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
	case capability.WindowsHostOSIdentitySnapshotID:
		f.hostRetained = retain
		return nil
	case capability.WindowsTransportEndpointSnapshotID:
		f.transportRetained = retain
		return nil
	case capability.WindowsEventLogSystemChannelID:
		if !f.eventReserved {
			return errors.New("file artifact was not reserved")
		}
		f.discardCalls++
		f.eventSealed = true
		if retain && !f.allowEventRetain {
			return errors.New("test Event Log artifact must not be retained in this seam")
		}
		if retain {
			f.eventRetained = true
			f.eventSealed = true
			return nil
		}
		return f.eventSealErr
	default:
		return errors.New("unexpected named artifact capability")
	}
}

func (f *fakeMultiArtifactSession) HashNamedArtifact(capabilityID string) (integrity.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if (capabilityID == capability.ProcessIdentitySnapshotID && !f.processRetained) || (capabilityID == capability.WindowsHostOSIdentitySnapshotID && !f.hostRetained) || (capabilityID == capability.WindowsTransportEndpointSnapshotID && !f.transportRetained) || (capabilityID == capability.WindowsEventLogSystemChannelID && !f.eventRetained) || (capabilityID != capability.ProcessIdentitySnapshotID && capabilityID != capability.WindowsHostOSIdentitySnapshotID && capabilityID != capability.WindowsTransportEndpointSnapshotID && capabilityID != capability.WindowsEventLogSystemChannelID) {
		return integrity.Entry{}, errors.New("process artifact was not retained")
	}
	digest := sha256.Sum256(f.buffer.Bytes())
	path := receipt.FirstStageArtifactPath
	if capabilityID == capability.WindowsHostOSIdentitySnapshotID {
		path = receipt.WindowsHostOSIdentityArtifactPath
	} else if capabilityID == capability.WindowsEventLogSystemChannelID {
		path = receipt.WindowsEventLogSystemArtifactPath
	} else if capabilityID == capability.WindowsTransportEndpointSnapshotID {
		path = receipt.WindowsTransportEndpointArtifactPath
	}
	return integrity.Entry{Path: path, Size: int64(f.buffer.Len()), SHA256: hex.EncodeToString(digest[:])}, nil
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

type acceptedFileArtifactRunner struct{ calls int }

func (r *acceptedFileArtifactRunner) Descriptor() provider.Descriptor {
	return (&cancellationFileArtifactRunner{}).Descriptor()
}
func (r *acceptedFileArtifactRunner) Artifact() provider.ArtifactDescriptor {
	return provider.ArtifactDescriptor{MediaType: receipt.WindowsEventLogSystemMediaType, ContentSchemaID: receipt.WindowsEventLogSystemSchemaID}
}
func (r *acceptedFileArtifactRunner) ExecuteToPath(_ context.Context, _ capability.Capability, _ fingerprint.TargetFingerprint, path string) execution.Result {
	r.calls++
	if err := os.WriteFile(path, []byte("EVTX-TEST"), 0o600); err != nil {
		return execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, SideEffectSummary: "test Event Log write failed"}
	}
	return execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "test Event Log Provider executed"}
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

func newRealHostTestStage(t *testing.T, process, host provider.StreamingRunner, event provider.FileArtifactRunner, factory firstStagePackageFactory) *FirstStage {
	t.Helper()
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: process, HostIdentityRunner: host, EventLogRunner: event,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) { return testFingerprint(), nil },
		PackageFactory:   factory, Clock: testNow, CollectionID: func() (string, error) { return testCollectionID, nil },
	})
	if err != nil {
		t.Fatalf("construct real host identity test stage: %v", err)
	}
	return stage
}

func newRealTransportTestStage(t *testing.T, process, host, transport provider.StreamingRunner, event provider.FileArtifactRunner, factory firstStagePackageFactory) *FirstStage {
	t.Helper()
	stage, err := newProcessIdentitySnapshotFirstStageWithDeps(time.Second, processIdentitySnapshotFirstStageDeps{
		StreamingRunner: process, HostIdentityRunner: host, TransportRunner: transport, EventLogRunner: event,
		FingerprintProbe: func(context.Context, string) (fingerprint.TargetFingerprint, error) { return testFingerprint(), nil },
		PackageFactory:   factory, Clock: testNow, CollectionID: func() (string, error) { return testCollectionID, nil },
	})
	if err != nil {
		t.Fatalf("construct real transport test stage: %v", err)
	}
	return stage
}

func newFakeTransportStreamingRunner() *fakeStreamingRunner {
	return &fakeStreamingRunner{
		descriptor: provider.Descriptor{
			ID: provider.WindowsTransportEndpointProviderID, Class: provider.FirstPartyNative,
			Capabilities: []string{capability.WindowsTransportEndpointSnapshotID},
			Requirements: provider.Requirements{Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"}, Architectures: []string{"amd64", "x86"}, Available: true, AvailabilityReason: execution.ReasonNone},
			Quality:      provider.Quality{Compatibility: execution.Available, Reason: execution.ReasonNone, Fidelity: 5, Completeness: 5, OutputStability: 5, EvidenceValue: 5},
		},
		artifact:           []byte(`{"protocol":"TCP","address_family":"IPv4","local_address":"127.0.0.1","local_port":1,"local_scope_id":null,"remote_address":null,"remote_port":null,"remote_scope_id":null,"tcp_state":2,"owning_pid":1}` + "\n"),
		artifactDescriptor: provider.ArtifactDescriptor{MediaType: provider.WindowsTransportEndpointMediaType, ContentSchemaID: provider.WindowsTransportEndpointSchemaID},
		result:             execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Fake transport snapshot completed."},
	}
}

func TestRealFirstStageNetworkSupplementalUsesV13AndRetainsOneArtifact(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	transport := newFakeTransportStreamingRunner()
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealTransportTestStage(t, process, host, transport, event, (&fakeMultiArtifactFactory{session: session}).begin)
	request := realRunRequest(t)
	request.Supplemental = []capability.CapabilityRequest{{ID: capability.WindowsTransportEndpointSnapshotID, Priority: capability.PriorityLate}}
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || len(result.Records) != 4 || !result.FinalizationVerified {
		t.Fatalf("result=%+v", result)
	}
	if session.metadata.SchemaVersion != receipt.FirstStageV13SchemaVersion || session.metadata.RuntimeArtifact != receipt.FirstStageV13RuntimeArtifact || len(session.metadata.ReceiptReferences) != 4 {
		t.Fatalf("metadata=%+v", session.metadata)
	}
	session.metadata.DirectoryLayout = []string{"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports"}
	if err := session.metadata.Validate(); err != nil {
		t.Fatalf("metadata validation=%v", err)
	}
	if len(session.metadata.ArtifactReferences) != 4 || !session.transportRetained {
		t.Fatalf("artifact refs=%+v transportRetained=%v", session.metadata.ArtifactReferences, session.transportRetained)
	}
	for _, record := range result.Records {
		if record.Request.ID == capability.WindowsTransportEndpointSnapshotID {
			if record.Request.Protected || record.ArtifactReference != receipt.WindowsTransportEndpointArtifactPath || record.Execution.State != execution.Collected {
				t.Fatalf("network record=%+v", record)
			}
		}
	}
}

func TestRealFirstStageNetworkRejectsLegacySessionBeforeOpenOrExecute(t *testing.T) {
	process := newFakeStreamingRunner()
	process.descriptor.Requirements.Available = false
	process.descriptor.Requirements.AvailabilityReason = execution.ReasonAPIUnavailable
	transport := newFakeTransportStreamingRunner()
	legacy := &fakeFirstStageSession{}
	factory := &fakeFirstStageFactory{session: legacy}
	stage := newRealTransportTestStage(t, process, nil, transport, nil, factory.begin)
	output := filepath.Join(t.TempDir(), "package")
	result, err := stage.Run(context.Background(), RunRequest{OutputDestination: output, Supplemental: []capability.CapabilityRequest{{ID: capability.WindowsTransportEndpointSnapshotID, Priority: capability.PriorityLate}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunFailed || result.OrchestrationReason != OrchestrationPackageFinalizationFailed {
		t.Fatalf("legacy network session result=%+v", result)
	}
	if legacy.openCalls != 0 || transport.calls != 0 || legacy.buffer.Len() != 0 || legacy.abortCalls != 1 {
		t.Fatalf("legacy network lifecycle open=%d execute=%d bytes=%d abort=%d", legacy.openCalls, transport.calls, legacy.buffer.Len(), legacy.abortCalls)
	}
	for _, record := range result.Records {
		if record.Request.ID == capability.WindowsTransportEndpointSnapshotID {
			if record.Attempted || record.ArtifactReference != "" || record.Execution.State != execution.Skipped {
				t.Fatalf("legacy network record=%+v", record)
			}
		}
	}
}

func TestRealFirstStageNetworkUnavailableStillFinalizesV13(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	transport := newFakeTransportStreamingRunner()
	transport.descriptor.Requirements.Available = false
	transport.descriptor.Requirements.AvailabilityReason = execution.ReasonAPIUnavailable
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealTransportTestStage(t, process, host, transport, event, (&fakeMultiArtifactFactory{session: session}).begin)
	request := realRunRequest(t)
	request.Supplemental = []capability.CapabilityRequest{{ID: capability.WindowsTransportEndpointSnapshotID, Priority: capability.PriorityLate}}
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || !result.FinalizationVerified || len(result.Records) != 4 {
		t.Fatalf("result=%+v", result)
	}
	if session.metadata.SchemaVersion != receipt.FirstStageV13SchemaVersion || len(session.metadata.ArtifactReferences) != 3 {
		t.Fatalf("metadata=%+v", session.metadata)
	}
	network := result.Records[3]
	if network.Request.ID != capability.WindowsTransportEndpointSnapshotID || network.Execution.State != execution.Skipped || network.Compatibility != execution.Unavailable || network.ArtifactReference != "" {
		t.Fatalf("network=%+v", network)
	}
}

func TestRealFirstStageDefaultWithTransportBindingRemainsV12(t *testing.T) {
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealTransportTestStage(t, newFakeStreamingRunner(), newFakeHostStreamingRunner(), newFakeTransportStreamingRunner(), &acceptedFileArtifactRunner{}, (&fakeMultiArtifactFactory{session: session}).begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || len(result.Records) != 3 {
		t.Fatalf("result=%+v", result)
	}
	if session.metadata.SchemaVersion != receipt.FirstStageV12SchemaVersion || session.metadata.RuntimeArtifact != receipt.FirstStageV12RuntimeArtifact || len(session.metadata.ReceiptReferences) != 3 {
		t.Fatalf("metadata=%+v", session.metadata)
	}
	session.metadata.DirectoryLayout = []string{"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports"}
	if err := session.metadata.Validate(); err != nil {
		t.Fatalf("v1.2 metadata with network descriptor rejected: %v", err)
	}
}

func TestRealFirstStageNetworkPackagePublishesAndVerifiesV13(t *testing.T) {
	stage := newRealTransportTestStage(t, newFakeStreamingRunner(), newFakeHostStreamingRunner(), newFakeTransportStreamingRunner(), &acceptedFileArtifactRunner{}, defaultFirstStagePackageFactory)
	output := filepath.Join(t.TempDir(), "package")
	request := RunRequest{OutputDestination: output, Supplemental: []capability.CapabilityRequest{{ID: capability.WindowsTransportEndpointSnapshotID, Priority: capability.PriorityLate}}}
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || !result.FinalizationVerified || len(result.Records) != 4 {
		t.Fatalf("result=%+v", result)
	}
	if err := integrity.Verify(output); err != nil {
		t.Fatalf("published v1.3 package failed verification: %v", err)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(output, "meta", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(metadataBytes, []byte(`"schema_version": "1.3"`)) || !bytes.Contains(metadataBytes, []byte(`derived/windows-transport-endpoints.ndjson`)) {
		t.Fatalf("metadata=%s", metadataBytes)
	}
}

func TestRealFirstStageNetworkFailureRetainsProtectedSurvivorsAndV13Accounting(t *testing.T) {
	transport := newFakeTransportStreamingRunner()
	transport.result = execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, SideEffectSummary: "bounded transport failure"}
	stage := newRealTransportTestStage(t, newFakeStreamingRunner(), newFakeHostStreamingRunner(), transport, &acceptedFileArtifactRunner{}, defaultFirstStagePackageFactory)
	output := filepath.Join(t.TempDir(), "package")
	result, err := stage.Run(context.Background(), RunRequest{OutputDestination: output, Supplemental: []capability.CapabilityRequest{{ID: capability.WindowsTransportEndpointSnapshotID, Priority: capability.PriorityLate}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || !result.FinalizationVerified || len(result.Records) != 4 {
		t.Fatalf("result=%+v", result)
	}
	for index, record := range result.Records {
		if index < 3 && (record.Execution.State != execution.Collected || record.ArtifactReference == "") {
			t.Fatalf("protected survivor[%d]=%+v", index, record)
		}
	}
	network := result.Records[3]
	if network.Execution.State != execution.Failed || network.Execution.Reason != execution.ReasonProviderError || network.ArtifactReference != "" {
		t.Fatalf("network failure=%+v", network)
	}
	if err := integrity.Verify(output); err != nil {
		t.Fatalf("partial v1.3 package failed verification: %v", err)
	}
}

func TestRealFirstStageDefaultPackageWithNetworkBindingStaysV12(t *testing.T) {
	stage := newRealTransportTestStage(t, newFakeStreamingRunner(), newFakeHostStreamingRunner(), newFakeTransportStreamingRunner(), &acceptedFileArtifactRunner{}, defaultFirstStagePackageFactory)
	output := filepath.Join(t.TempDir(), "package")
	result, err := stage.Run(context.Background(), RunRequest{OutputDestination: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || len(result.Records) != 3 || !result.FinalizationVerified {
		t.Fatalf("result=%+v", result)
	}
	if err := integrity.Verify(output); err != nil {
		t.Fatalf("published default package failed verification: %v", err)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(output, "meta", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(metadataBytes, []byte(`"schema_version": "1.2"`)) || bytes.Contains(metadataBytes, []byte(`windows-transport-endpoints`)) {
		t.Fatalf("metadata=%s", metadataBytes)
	}
}

func TestRealFirstStageProtectedHostUsesV12AndRetainsBaselineArtifacts(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealHostTestStage(t, process, host, event, (&fakeMultiArtifactFactory{session: session}).begin)
	request := realRunRequest(t)
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunComplete || !result.FinalizationVerified || len(result.Records) != 3 {
		t.Fatalf("result=%+v", result)
	}
	if session.metadata.SchemaVersion != receipt.FirstStageV12SchemaVersion || session.metadata.RuntimeArtifact != receipt.FirstStageV12RuntimeArtifact || len(session.metadata.ReceiptReferences) != 3 {
		t.Fatalf("metadata=%+v", session.metadata)
	}
	if session.metadata.ArtifactReferences == nil || len(session.metadata.ArtifactReferences) != 3 {
		t.Fatalf("artifact refs=%+v", session.metadata.ArtifactReferences)
	}
	if host.calls != 1 || !session.hostRetained || !session.processRetained || !session.eventRetained {
		t.Fatalf("retention host=%v process=%v event=%v calls=%d", session.hostRetained, session.processRetained, session.eventRetained, host.calls)
	}
	for _, record := range result.Records {
		if record.Request.ID == capability.WindowsHostOSIdentitySnapshotID && !record.Request.Protected {
			t.Fatalf("host request was not protected: %+v", record.Request)
		}
	}
}

func TestRealFirstStageHostFailurePreservesBaselineAndOmitsHostArtifact(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	host.result = execution.Result{State: execution.Failed, Reason: execution.ReasonProviderError, SideEffectSummary: "Fake host failure."}
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealHostTestStage(t, process, host, event, (&fakeMultiArtifactFactory{session: session}).begin)
	request := realRunRequest(t)
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || !result.FinalizationVerified || len(result.Records) != 3 {
		t.Fatalf("result=%+v", result)
	}
	for _, record := range result.Records {
		switch record.Request.ID {
		case capability.ProcessIdentitySnapshotID, capability.WindowsEventLogSystemChannelID:
			if record.Execution.State != execution.Collected || record.ArtifactReference == "" {
				t.Fatalf("baseline record=%+v", record)
			}
		case capability.WindowsHostOSIdentitySnapshotID:
			if !record.Request.Protected || record.Execution.State != execution.Failed || record.ArtifactReference != "" || len(record.MissingEvidence) == 0 {
				t.Fatalf("host record=%+v", record)
			}
		}
	}
	if len(session.metadata.ArtifactReferences) != 2 || session.hostRetained {
		t.Fatalf("metadata=%+v hostRetained=%v", session.metadata, session.hostRetained)
	}
}

func TestRealFirstStageProtectedHostUnavailablePreservesBaseline(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	host.descriptor.Requirements.Available = false
	host.descriptor.Requirements.AvailabilityReason = execution.ReasonAPIUnavailable
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealHostTestStage(t, process, host, event, (&fakeMultiArtifactFactory{session: session}).begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || !result.FinalizationVerified || len(result.Records) != 3 {
		t.Fatalf("result=%+v", result)
	}
	for _, record := range result.Records {
		switch record.Request.ID {
		case capability.ProcessIdentitySnapshotID, capability.WindowsEventLogSystemChannelID:
			if record.Execution.State != execution.Collected || record.ArtifactReference == "" {
				t.Fatalf("baseline record=%+v", record)
			}
		case capability.WindowsHostOSIdentitySnapshotID:
			if !record.Request.Protected || record.Compatibility != execution.Unavailable || record.Execution.State != execution.Skipped || record.ArtifactReference != "" {
				t.Fatalf("unavailable host record=%+v", record)
			}
		}
	}
	if len(session.metadata.ArtifactReferences) != 2 || session.hostRetained {
		t.Fatalf("metadata=%+v hostRetained=%v", session.metadata, session.hostRetained)
	}
}

func TestRealFirstStageProtectedHostBlockedPreservesBaseline(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	host.result = execution.Result{State: execution.Blocked, Reason: execution.ReasonPrivilegeRequired, SideEffectSummary: "Fake host access was blocked."}
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealHostTestStage(t, process, host, event, (&fakeMultiArtifactFactory{session: session}).begin)
	result, err := stage.Run(context.Background(), realRunRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || !result.FinalizationVerified || len(result.Records) != 3 {
		t.Fatalf("result=%+v", result)
	}
	hostRecord := result.Records[2]
	if hostRecord.Request.ID != capability.WindowsHostOSIdentitySnapshotID || !hostRecord.Request.Protected || hostRecord.Execution.State != execution.Blocked || hostRecord.Execution.Reason != execution.ReasonPrivilegeRequired || hostRecord.ArtifactReference != "" {
		t.Fatalf("blocked host record=%+v", hostRecord)
	}
	if result.Records[0].Execution.State != execution.Collected || result.Records[1].Execution.State != execution.Collected || len(session.metadata.ArtifactReferences) != 2 || session.hostRetained {
		t.Fatalf("baseline retention result=%+v metadata=%+v hostRetained=%v", result.Records, session.metadata, session.hostRetained)
	}
}

func TestRealFirstStageHostCancellationDiscardsHostArtifact(t *testing.T) {
	process := newFakeStreamingRunner()
	host := newFakeHostStreamingRunner()
	host.execute = func(_ context.Context, writer io.Writer) execution.Result {
		if _, err := writer.Write([]byte(`{"computer_name":"HOST"}`)); err != nil {
			t.Fatalf("host test write: %v", err)
		}
		return execution.Result{State: execution.Failed, Reason: execution.ReasonCancelled, SideEffectSummary: "Fake host cancellation."}
	}
	event := &acceptedFileArtifactRunner{}
	session := &fakeMultiArtifactSession{stagingPath: filepath.Join(t.TempDir(), "system.evtx"), allowEventRetain: true}
	stage := newRealHostTestStage(t, process, host, event, (&fakeMultiArtifactFactory{session: session}).begin)
	request := realRunRequest(t)
	result, err := stage.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RunPartial || !result.FinalizationVerified || len(session.metadata.ArtifactReferences) != 2 || session.hostRetained {
		t.Fatalf("result=%+v metadata=%+v hostRetained=%v", result, session.metadata, session.hostRetained)
	}
	for _, record := range result.Records {
		if record.Request.ID == capability.WindowsHostOSIdentitySnapshotID && (record.Execution.State != execution.Failed || record.Execution.Reason != execution.ReasonCancelled || record.ArtifactReference != "") {
			t.Fatalf("host cancellation record=%+v", record)
		}
	}
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

func newFakeHostStreamingRunner() *fakeStreamingRunner {
	return &fakeStreamingRunner{
		descriptor: provider.Descriptor{
			ID: provider.WindowsHostOSIdentityProviderID, Class: provider.FirstPartyNative,
			Capabilities: []string{capability.WindowsHostOSIdentitySnapshotID},
			Requirements: provider.Requirements{Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"}, Architectures: []string{"amd64"}, Available: true, AvailabilityReason: execution.ReasonNone},
			Quality:      provider.Quality{Compatibility: execution.Available, Reason: execution.ReasonNone, Fidelity: 4, Completeness: 4, OutputStability: 5, EvidenceValue: 4},
		},
		artifact:           []byte(`{"computer_name":"HOST","os_major":10,"os_minor":0,"os_build":1,"native_architecture":"amd64"}`),
		artifactDescriptor: provider.ArtifactDescriptor{MediaType: provider.WindowsHostOSIdentityMediaType, ContentSchemaID: provider.WindowsHostOSIdentitySchemaID},
		result:             execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Fake host identity completed."},
	}
}

func realRunRequest(t *testing.T) RunRequest {
	t.Helper()
	return RunRequest{CaseID: "CASE-REAL-TEST", OutputDestination: filepath.Join(t.TempDir(), "first-stage-package")}
}
