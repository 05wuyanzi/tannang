//go:build windows

// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

const (
	firstStageSchemaHandoffRootEnv = "TANNANG_FIRSTSTAGE_SCHEMA_HANDOFF_ROOT"
	firstStageMetadataInstance     = "first-stage-evidence-package-v0.instance.json"
	firstStageReceiptInstance      = "first-stage-receipt-v0.instance.json"
)

func TestWindowsFirstStageProcessIdentitySnapshotAcceptance(t *testing.T) {
	if os.Getenv("TANNANG_RUN_FIRSTSTAGE_PROCESS_SNAPSHOT_ACCEPTANCE") != "1" {
		t.Skip("set TANNANG_RUN_FIRSTSTAGE_PROCESS_SNAPSHOT_ACCEPTANCE=1 for the bounded real FirstStage acceptance")
	}
	handoffRoot, err := firstStageSchemaHandoffRoot(os.Getenv(firstStageSchemaHandoffRootEnv))
	if err != nil {
		t.Fatalf("validate schema handoff root: %v", err)
	}
	output := filepath.Join(t.TempDir(), "first-stage-process-snapshot")
	stage, err := NewProcessIdentitySnapshotFirstStage(30 * time.Second)
	if err != nil {
		t.Fatalf("construct production FirstStage: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	result, err := stage.Run(ctx, RunRequest{OutputDestination: output})
	if err != nil {
		t.Fatalf("real FirstStage Run error: %v", err)
	}
	if result.State != RunComplete || !result.FinalizationVerified || result.PackageReference != output || len(result.Records) != 1 || !result.Records[0].Request.Protected {
		t.Fatalf("unexpected bounded acceptance result: state=%s verified=%v records=%d", result.State, result.FinalizationVerified, len(result.Records))
	}
	metadata, metadataBytes := decodeFirstStageAcceptanceJSON[receipt.FirstStagePackageMetadata](t, output, "meta/package.json")
	if err := metadata.Validate(); err != nil {
		t.Fatalf("metadata validation failed: %v", err)
	}
	if len(metadata.ReceiptReferences) != 1 || metadata.ReceiptReferences[0] != receipt.FirstStageReceiptPath(result.Records[0].Request.ID) || len(metadata.ArtifactReferences) != 1 {
		t.Fatalf("metadata references are incomplete: %+v", metadata)
	}
	diskReceipt, receiptBytes := decodeFirstStageAcceptanceJSON[receipt.FirstStageRecord](t, output, metadata.ReceiptReferences[0])
	if err := diskReceipt.Validate(); err != nil {
		t.Fatalf("receipt validation failed: %v", err)
	}
	runRecord := result.Records[0]
	if err := runRecord.Validate(); err != nil {
		t.Fatalf("run capability record validation failed: %v", err)
	}
	if diskReceipt.CollectionID != result.Context.CollectionID ||
		!reflect.DeepEqual(diskReceipt.RequestedCapability, runRecord.Request) ||
		!reflect.DeepEqual(diskReceipt.Capability, runRecord.Capability) ||
		diskReceipt.SelectedProvider == nil || runRecord.SelectedProvider == nil ||
		diskReceipt.SelectedProvider.ID != runRecord.SelectedProvider.ID ||
		diskReceipt.SelectedProvider.Class != runRecord.SelectedProvider.Class ||
		diskReceipt.Compatibility != runRecord.Compatibility || runRecord.Decision == nil ||
		diskReceipt.CompatibilityReason == nil || *diskReceipt.CompatibilityReason != runRecord.Decision.Reason ||
		!reflect.DeepEqual(diskReceipt.CandidateEvaluations, runRecord.Decision.Evaluations) ||
		diskReceipt.Attempted != runRecord.Attempted || !reflect.DeepEqual(diskReceipt.Execution, runRecord.Execution) {
		t.Fatalf("receipt attribution does not match the real Run: run=%+v receipt=%+v", runRecord, diskReceipt)
	}
	if diskReceipt.ArtifactReference == nil || diskReceipt.ArtifactReference.Path != receipt.FirstStageArtifactPath || diskReceipt.ArtifactReference.RawOrDerived != "DERIVED" {
		t.Fatalf("receipt artifact reference is incomplete: %+v", diskReceipt.ArtifactReference)
	}
	if metadata.ArtifactReferences[0] != *diskReceipt.ArtifactReference {
		t.Fatalf("metadata and receipt artifact references differ: metadata=%+v receipt=%+v", metadata.ArtifactReferences[0], diskReceipt.ArtifactReference)
	}
	if result.Records[0].ReceiptReference != metadata.ReceiptReferences[0] || result.Records[0].ArtifactReference != diskReceipt.ArtifactReference.Path {
		t.Fatalf("run references do not match disk references: result=%+v receipt=%+v", result.Records[0], diskReceipt)
	}
	if err := integrity.Verify(output); err != nil {
		t.Fatalf("verify published package: %v", err)
	}
	file, err := pathsafe.OpenFile(output, receipt.FirstStageArtifactPath)
	if err != nil {
		t.Fatalf("open bounded artifact: %v", err)
	}
	defer file.Close()
	type row struct {
		ProcessID       uint32 `json:"process_id"`
		ParentProcessID uint32 `json:"parent_process_id"`
		ExecutableName  string `json:"executable_name"`
	}
	seen := make(map[uint32]struct{})
	foundCurrent := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record row
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil || record.ExecutableName == "" {
			t.Fatalf("invalid process identity row")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			t.Fatalf("process identity row contains trailing data: %v", err)
		}
		if _, duplicate := seen[record.ProcessID]; duplicate {
			t.Fatalf("duplicate process id %d", record.ProcessID)
		}
		seen[record.ProcessID] = struct{}{}
		foundCurrent = foundCurrent || record.ProcessID == uint32(os.Getpid())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan bounded artifact: %v", err)
	}
	if len(seen) == 0 || !foundCurrent {
		t.Fatalf("bounded artifact count=%d current_pid_present=%v", len(seen), foundCurrent)
	}
	entry, err := integrity.HashFile(output, receipt.FirstStageArtifactPath)
	if err != nil {
		t.Fatalf("hash bounded artifact: %v", err)
	}
	if entry.Size != metadata.ArtifactReferences[0].Size || entry.SHA256 != metadata.ArtifactReferences[0].SHA256 || entry.Size != diskReceipt.ArtifactReference.Size || entry.SHA256 != diskReceipt.ArtifactReference.SHA256 {
		t.Fatalf("artifact hash/reference mismatch: entry=%+v metadata=%+v receipt=%+v", entry, metadata.ArtifactReferences[0], diskReceipt.ArtifactReference)
	}
	manifest, _ := decodeFirstStageAcceptanceJSON[integrity.Manifest](t, output, integrity.ManifestPath)
	var manifestArtifact *integrity.Entry
	manifestPaths := make(map[string]struct{}, len(manifest.Entries))
	receiptManifested := false
	for i := range manifest.Entries {
		if _, duplicate := manifestPaths[manifest.Entries[i].Path]; duplicate {
			t.Fatalf("duplicate manifest path %q", manifest.Entries[i].Path)
		}
		manifestPaths[manifest.Entries[i].Path] = struct{}{}
		if manifest.Entries[i].Path == receipt.FirstStageArtifactPath {
			manifestArtifact = &manifest.Entries[i]
		}
		receiptManifested = receiptManifested || manifest.Entries[i].Path == metadata.ReceiptReferences[0]
	}
	if manifestArtifact == nil || manifestArtifact.Path != diskReceipt.ArtifactReference.Path || manifestArtifact.Size != entry.Size || manifestArtifact.SHA256 != entry.SHA256 || !receiptManifested {
		t.Fatalf("manifest artifact entry mismatch: %+v", manifestArtifact)
	}
	if err := handoffFirstStageAcceptanceJSON(handoffRoot, metadataBytes, receiptBytes); err != nil {
		t.Fatalf("write exact schema-validation instances: %v", err)
	}
	t.Logf("process_snapshot count=%d bytes=%d elapsed=%s sha256=%s state=%s", len(seen), entry.Size, time.Since(started), entry.SHA256, result.State)
}

func decodeFirstStageAcceptanceJSON[T any](t *testing.T, root, relative string) (T, []byte) {
	t.Helper()
	data, err := pathsafe.ReadFile(root, relative)
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", relative, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("decode %s trailing data: %v", relative, err)
	}
	return value, data
}

// The outer opt-in acceptance Gate creates and removes this unique root. Go
// only hands off the exact bytes it just decoded; Draft 2020-12 validation is
// intentionally performed by the Gate's offline validator, not by this test.
func firstStageSchemaHandoffRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("schema handoff root is required for real FirstStage acceptance")
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", errors.New("schema handoff root must be an absolute clean path")
	}
	if err := pathsafe.ValidatePackageTree(value); err != nil {
		return "", fmt.Errorf("validate schema handoff root: %w", err)
	}
	repository, err := firstStageRepositoryRoot()
	if err != nil {
		return "", err
	}
	insideRepository, err := firstStagePathWithin(repository, value)
	if err != nil {
		return "", err
	}
	if insideRepository {
		return "", errors.New("schema handoff root must be outside the Tannang repository")
	}
	entries, err := os.ReadDir(value)
	if err != nil {
		return "", fmt.Errorf("read schema handoff root: %w", err)
	}
	if len(entries) != 0 {
		return "", errors.New("schema handoff root must be empty")
	}
	return value, nil
}

func firstStageRepositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get test working directory: %w", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(directory, "go.mod")); statErr == nil && info.Mode().IsRegular() {
			return filepath.EvalSymlinks(directory)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("locate Tannang repository root")
		}
		directory = parent
	}
}

func firstStagePathWithin(parent, candidate string) (bool, error) {
	if !strings.EqualFold(filepath.VolumeName(parent), filepath.VolumeName(candidate)) {
		return false, nil
	}
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false, fmt.Errorf("compare schema handoff root to repository: %w", err)
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)), nil
}

func handoffFirstStageAcceptanceJSON(root string, metadataBytes, receiptBytes []byte) error {
	instances := []struct {
		name string
		data []byte
	}{
		{name: firstStageMetadataInstance, data: metadataBytes},
		{name: firstStageReceiptInstance, data: receiptBytes},
	}
	for _, instance := range instances {
		if err := pathsafe.WriteNewFile(root, instance.name, instance.data, 0o600); err != nil {
			return fmt.Errorf("write schema handoff %s: %w", instance.name, err)
		}
		stored, err := pathsafe.ReadFile(root, instance.name)
		if err != nil {
			return fmt.Errorf("read schema handoff %s: %w", instance.name, err)
		}
		if len(stored) != len(instance.data) || !bytes.Equal(stored, instance.data) {
			return fmt.Errorf("schema handoff %s bytes differ from the accepted package bytes", instance.name)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read completed schema handoff root: %w", err)
	}
	if len(entries) != len(instances) {
		return errors.New("completed schema handoff root contains unexpected entries")
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return fmt.Errorf("schema handoff %s is not a regular file", entry.Name())
		}
		seen[entry.Name()] = struct{}{}
	}
	for _, instance := range instances {
		if _, ok := seen[instance.name]; !ok {
			return fmt.Errorf("completed schema handoff is missing %s", instance.name)
		}
	}
	return nil
}

func TestWindowsFirstStageSchemaHandoffContract(t *testing.T) {
	if os.Getenv("TANNANG_RUN_FIRSTSTAGE_PROCESS_SNAPSHOT_ACCEPTANCE") == "1" {
		t.Skip("handoff contract test does not run beside real FirstStage acceptance")
	}
	if _, err := firstStageSchemaHandoffRoot(""); err == nil {
		t.Fatal("missing handoff root unexpectedly accepted")
	}
	if _, err := firstStageSchemaHandoffRoot("relative"); err == nil {
		t.Fatal("relative handoff root unexpectedly accepted")
	}
	if _, err := firstStageSchemaHandoffRoot(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("nonexistent handoff root unexpectedly accepted")
	}

	nonEmpty := t.TempDir()
	if err := pathsafe.WriteNewFile(nonEmpty, "existing", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := firstStageSchemaHandoffRoot(nonEmpty); err == nil {
		t.Fatal("non-empty handoff root unexpectedly accepted")
	}

	repository, err := firstStageRepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstStageSchemaHandoffRoot(filepath.Join(repository, "internal")); err == nil {
		t.Fatal("repository-contained handoff root unexpectedly accepted")
	}

	root, err := firstStageSchemaHandoffRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata := []byte("{\n  \"metadata\": true\n}\n")
	receipt := []byte("{\n  \"receipt\": true\n}\n")
	if err := handoffFirstStageAcceptanceJSON(root, metadata, receipt); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{firstStageMetadataInstance: metadata, firstStageReceiptInstance: receipt} {
		got, err := pathsafe.ReadFile(root, name)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("handoff %s exact bytes = %q, %v; want %q", name, got, err, want)
		}
	}
	if err := handoffFirstStageAcceptanceJSON(root, metadata, receipt); err == nil {
		t.Fatal("existing handoff destinations unexpectedly overwritten")
	}
}
