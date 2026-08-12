// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/receipt"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

type injectedArtifactFile struct {
	syncErr    error
	closeErr   error
	syncCalls  int
	closeCalls int
}

func (f *injectedArtifactFile) Write(data []byte) (int, error) { return len(data), nil }
func (f *injectedArtifactFile) Sync() error {
	f.syncCalls++
	return f.syncErr
}
func (f *injectedArtifactFile) Close() error {
	f.closeCalls++
	return f.closeErr
}

func TestFirstStagePackageSessionFinalizesAndPreservesPublishedPackageOnAbort(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	session, err := BeginFirstStagePackage(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := session.OpenArtifact()
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte("{\"process_id\":1,\"parent_process_id\":0,\"executable_name\":\"fixture.exe\"}\n")
	if _, err := writer.Write(artifact); err != nil {
		t.Fatal(err)
	}
	if err := session.SealArtifact(true); err != nil {
		t.Fatal(err)
	}
	entry, err := session.HashArtifact()
	if err != nil {
		t.Fatal(err)
	}
	reference := receipt.ArtifactReference{Path: entry.Path, MediaType: receipt.FirstStageArtifactMedia, ContentSchemaID: receipt.FirstStageArtifactSchema, RawOrDerived: "DERIVED", Size: entry.Size, SHA256: entry.SHA256}
	record := validFirstStageRecord(reference)
	metadata := validFirstStageMetadata(reference)
	if err := session.Finalize(context.Background(), metadata, []receipt.FirstStageRecord{record}); err != nil {
		t.Fatal(err)
	}
	if err := integrity.Verify(output); err != nil {
		t.Fatalf("published package verify: %v", err)
	}
	metadataBytes, err := pathsafe.ReadFile(output, "meta/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var diskMetadata receipt.FirstStagePackageMetadata
	if err := json.Unmarshal(metadataBytes, &diskMetadata); err != nil {
		t.Fatal(err)
	}
	if diskMetadata.StartedAt != metadata.StartedAt || diskMetadata.FinishedAt != metadata.FinishedAt || len(diskMetadata.ArtifactReferences) != 1 || diskMetadata.ArtifactReferences[0] != reference {
		t.Fatalf("disk metadata changed or lost artifact reference: %+v", diskMetadata)
	}
	receiptBytes, err := pathsafe.ReadFile(output, receipt.FirstStageReceiptPath(capability.ProcessIdentitySnapshotID))
	if err != nil {
		t.Fatal(err)
	}
	var diskReceipt receipt.FirstStageRecord
	if err := json.Unmarshal(receiptBytes, &diskReceipt); err != nil {
		t.Fatal(err)
	}
	if diskReceipt.ArtifactReference == nil || *diskReceipt.ArtifactReference != reference || diskReceipt.AcquisitionStartedAt != metadata.StartedAt || diskReceipt.AcquisitionFinishedAt != metadata.FinishedAt {
		t.Fatalf("disk receipt changed or lost artifact reference: %+v", diskReceipt)
	}
	manifestBytes, err := pathsafe.ReadFile(output, integrity.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest integrity.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	var manifestEntry integrity.Entry
	for _, candidate := range manifest.Entries {
		if candidate.Path == reference.Path {
			manifestEntry = candidate
			break
		}
	}
	if manifestEntry != (integrity.Entry{Path: reference.Path, Size: reference.Size, SHA256: reference.SHA256}) {
		t.Fatalf("manifest artifact entry mismatch: %+v", manifestEntry)
	}
	secondHash, err := integrity.HashFile(output, reference.Path)
	if err != nil || secondHash.Size != reference.Size || secondHash.SHA256 != reference.SHA256 {
		t.Fatalf("second artifact hash mismatch: entry=%+v err=%v", secondHash, err)
	}
	before, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(receipt.FirstStageArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Abort(); err != nil {
		t.Fatalf("post-publish Abort: %v", err)
	}
	if err := session.Abort(); err != nil {
		t.Fatalf("cached post-publish Abort: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(receipt.FirstStageArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || !session.published || !session.abortDone {
		t.Fatal("post-publish Abort mutated the published package or lost terminal state")
	}
}

func TestFirstStagePackageSessionAbortCachesSuccess(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	session, err := BeginFirstStagePackage(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := session.OpenArtifact()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("candidate")); err != nil {
		t.Fatal(err)
	}
	if err := session.SealArtifact(true); err != nil {
		t.Fatal(err)
	}
	staging := session.root.Path()
	if err := session.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging remains after successful Abort: %v", err)
	}
	if err := session.Abort(); err != nil || !session.abortDone || session.abortError != nil {
		t.Fatalf("second Abort did not return cached success: %v", err)
	}
}

func TestFirstStagePackageSessionAbortCachesPartialCleanupFailure(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	session, err := BeginFirstStagePackage(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := session.OpenArtifact()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("candidate")); err != nil {
		t.Fatal(err)
	}
	if err := session.SealArtifact(false); err != nil {
		t.Fatal(err)
	}
	unsafeTarget := filepath.Join(session.root.Path(), "reports", "redirect")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), unsafeTarget); err != nil {
		t.Skipf("symlink oracle unavailable: %v", err)
	}
	first := session.Abort()
	if first == nil || !session.residualPossible || !session.removed {
		t.Fatalf("partial cleanup failure truth missing: err=%v removed=%v residual=%v", first, session.removed, session.residualPossible)
	}
	second := session.Abort()
	if second == nil || second.Error() != first.Error() {
		t.Fatalf("second Abort did not return cached failure: first=%v second=%v", first, second)
	}
	if _, err := os.Stat(filepath.Join(session.root.Path(), filepath.FromSlash(receipt.FirstStageArtifactPath))); !os.IsNotExist(err) {
		t.Fatalf("already-removed artifact reappeared: %v", err)
	}
}

func TestFirstStagePackageSessionSyncAndCloseFailuresAreTerminal(t *testing.T) {
	for name, configure := range map[string]func(*injectedArtifactFile){
		"sync":  func(file *injectedArtifactFile) { file.syncErr = errors.New("injected sync failure") },
		"close": func(file *injectedArtifactFile) { file.closeErr = errors.New("injected close failure") },
	} {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			session, err := BeginFirstStagePackage(context.Background(), filepath.Join(t.TempDir(), "package"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.root.Cleanup() })
			if _, err := session.OpenArtifact(); err != nil {
				t.Fatal(err)
			}
			if err := session.artifact.file.Close(); err != nil {
				t.Fatal(err)
			}
			injected := &injectedArtifactFile{}
			configure(injected)
			session.artifact.file = injected
			if err := session.SealArtifact(true); err == nil {
				t.Fatalf("%s failure was not reported", name)
			}
			first := session.Abort()
			second := session.Abort()
			if first == nil || second == nil || first.Error() != second.Error() || !session.residualPossible || injected.syncCalls != 1 || injected.closeCalls != 1 {
				t.Fatalf("%s terminal state first=%v second=%v residual=%v sync=%d close=%d", name, first, second, session.residualPossible, injected.syncCalls, injected.closeCalls)
			}
		})
	}
}

func TestFirstStagePackageSessionRemoveFailureIsCachedWithoutRetry(t *testing.T) {
	session, err := BeginFirstStagePackage(context.Background(), filepath.Join(t.TempDir(), "package"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.root.Cleanup() })
	if _, err := session.OpenArtifact(); err != nil {
		t.Fatal(err)
	}
	if err := session.artifact.file.Close(); err != nil {
		t.Fatal(err)
	}
	session.artifact.file = nil
	artifactPath := filepath.Join(session.root.Path(), filepath.FromSlash(receipt.FirstStageArtifactPath))
	if err := os.Remove(artifactPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(artifactPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := session.SealArtifact(false); err == nil {
		t.Fatal("directory replacement did not make exact-file removal fail closed")
	}
	first := session.Abort()
	second := session.Abort()
	info, statErr := os.Stat(artifactPath)
	if first == nil || second == nil || first.Error() != second.Error() || !session.removeAttempted || session.removed || !session.residualPossible || statErr != nil || !info.IsDir() {
		t.Fatalf("remove failure state first=%v second=%v attempted=%v removed=%v residual=%v info=%v stat=%v", first, second, session.removeAttempted, session.removed, session.residualPossible, info, statErr)
	}
}

func TestFirstStagePackageSessionFinalizationFailureClasses(t *testing.T) {
	for _, name := range []string{"metadata", "receipt", "manifest generate", "publish"} {
		name := name
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "package")
			session, err := BeginFirstStagePackage(context.Background(), output)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.root.Cleanup() })
			metadata := validReceiptOnlyMetadata()
			record := validReceiptOnlyRecord()
			switch name {
			case "metadata":
				metadata.FinishedAt = ""
			case "receipt":
				record.Execution.Payload = []byte("forbidden")
			case "manifest generate":
				if err := session.root.WriteFile(integrity.ManifestPath, []byte("{}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "publish":
				if err := os.Mkdir(output, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := session.Finalize(context.Background(), metadata, []receipt.FirstStageRecord{record}); err == nil {
				t.Fatalf("%s failure was not reported", name)
			}
			if err := session.Abort(); err != nil && !session.residualPossible {
				t.Fatalf("%s Abort lost residual truth: %v", name, err)
			}
		})
	}
}

func validFirstStageRecord(reference receipt.ArtifactReference) receipt.FirstStageRecord {
	definition := capability.ProcessIdentitySnapshot()
	request := capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityNormal, Protected: true}
	reason := execution.ReasonNone
	return receipt.FirstStageRecord{
		SchemaVersion: receipt.SchemaVersion, ManifestVersion: receipt.ManifestVersion, ProductVersion: receipt.ProductVersion,
		RuntimeArtifact: receipt.FirstStageRuntimeArtifact, CollectionID: "COL-00000000-0000-4000-8000-000000000001", CaseID: "CASE-01",
		TargetFingerprint: validPackageFingerprint(), RequestedCapability: request, Capability: &definition,
		SelectedProvider: &receipt.ProviderIdentity{ID: "windows-toolhelp-process-snapshot", Class: provider.FirstPartyNative},
		Compatibility:    execution.Available, CompatibilityReason: &reason,
		CandidateEvaluations: []resolver.CandidateEvaluation{{ProviderID: "windows-toolhelp-process-snapshot", Compatibility: execution.Available, Reason: execution.ReasonNone, Eligible: true}},
		Attempted:            true, Execution: execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Fixed package test execution."},
		ArtifactReference: &reference, AcquisitionStartedAt: packageTestTime().Format(time.RFC3339Nano), AcquisitionFinishedAt: packageTestTime().Add(time.Second).Format(time.RFC3339Nano),
	}
}

func validFirstStageMetadata(reference receipt.ArtifactReference) receipt.FirstStagePackageMetadata {
	return receipt.FirstStagePackageMetadata{
		SchemaVersion: receipt.SchemaVersion, ManifestVersion: receipt.ManifestVersion, ProductVersion: receipt.ProductVersion,
		RuntimeArtifact: receipt.FirstStageRuntimeArtifact, CollectionID: "COL-00000000-0000-4000-8000-000000000001", CaseID: "CASE-01",
		StartedAt: packageTestTime().Format(time.RFC3339Nano), FinishedAt: packageTestTime().Add(time.Second).Format(time.RFC3339Nano),
		TargetFingerprint: validPackageFingerprint(), RunState: "COMPLETE",
		ReceiptReferences: []string{receipt.FirstStageReceiptPath(capability.ProcessIdentitySnapshotID)}, ArtifactReferences: []receipt.ArtifactReference{reference},
	}
}

func validReceiptOnlyRecord() receipt.FirstStageRecord {
	definition := capability.ProcessIdentitySnapshot()
	request := capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityNormal, Protected: true}
	reason := execution.ReasonAPIUnavailable
	return receipt.FirstStageRecord{
		SchemaVersion: receipt.SchemaVersion, ManifestVersion: receipt.ManifestVersion, ProductVersion: receipt.ProductVersion,
		RuntimeArtifact: receipt.FirstStageRuntimeArtifact, CollectionID: "COL-00000000-0000-4000-8000-000000000001", CaseID: "CASE-01",
		TargetFingerprint: validPackageFingerprint(), RequestedCapability: request, Capability: &definition,
		Compatibility: execution.Unavailable, CompatibilityReason: &reason,
		Attempted: false, Execution: execution.Result{State: execution.Skipped, Reason: execution.ReasonAPIUnavailable, SideEffectSummary: "No Provider was executed."},
		MissingEvidence: []string{"No suitable Provider was available."}, AcquisitionStartedAt: packageTestTime().Format(time.RFC3339Nano), AcquisitionFinishedAt: packageTestTime().Add(time.Second).Format(time.RFC3339Nano),
	}
}

func validReceiptOnlyMetadata() receipt.FirstStagePackageMetadata {
	return receipt.FirstStagePackageMetadata{
		SchemaVersion: receipt.SchemaVersion, ManifestVersion: receipt.ManifestVersion, ProductVersion: receipt.ProductVersion,
		RuntimeArtifact: receipt.FirstStageRuntimeArtifact, CollectionID: "COL-00000000-0000-4000-8000-000000000001", CaseID: "CASE-01",
		StartedAt: packageTestTime().Format(time.RFC3339Nano), FinishedAt: packageTestTime().Add(time.Second).Format(time.RFC3339Nano),
		TargetFingerprint: validPackageFingerprint(), RunState: "PARTIAL",
		ReceiptReferences: []string{receipt.FirstStageReceiptPath(capability.ProcessIdentitySnapshotID)}, ArtifactReferences: []receipt.ArtifactReference{},
	}
}

func validPackageFingerprint() fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "synthetic", Build: "0", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN"}
}

func packageTestTime() time.Time {
	return time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
}
