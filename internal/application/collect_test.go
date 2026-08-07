// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/05wuyanzi/tannang/internal/application"
	"github.com/05wuyanzi/tannang/internal/evidence"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

func TestSyntheticEndToEndScenarios(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		compatibility execution.CompatibilityState
		state         execution.State
		reason        execution.Reason
		hasArtifact   bool
	}{
		{"available-collected", execution.Available, execution.Collected, execution.ReasonNone, true},
		{"degraded-partial", execution.Degraded, execution.Partial, execution.ReasonAPIUnavailable, true},
		{"unavailable", execution.Unavailable, execution.Skipped, execution.ReasonDependencyMissing, false},
		{"privilege-blocked", execution.Degraded, execution.Blocked, execution.ReasonPrivilegeRequired, false},
		{"provider-failure", execution.Available, execution.Failed, execution.ReasonProviderError, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output := filepath.Join(t.TempDir(), test.name)
			outcome, err := application.Collect(context.Background(), test.name, output)
			if err != nil {
				t.Fatalf("Collect() error: %v", err)
			}
			if outcome.Record.Compatibility != test.compatibility || outcome.Record.Execution.State != test.state || outcome.Record.Reason != test.reason {
				t.Fatalf("unexpected outcome: compatibility=%s execution=%s reason=%s", outcome.Record.Compatibility, outcome.Record.Execution.State, outcome.Record.Reason)
			}
			if err := integrity.Verify(output); err != nil {
				t.Fatalf("Verify() error: %v", err)
			}
			assertPackageLayout(t, output)
			metadata := readRecord(t, output)
			if metadata.Execution.State != test.state || metadata.Compatibility != test.compatibility {
				t.Fatalf("metadata did not preserve status separation: %+v", metadata)
			}
			if test.hasArtifact {
				if metadata.ArtifactPath == "" {
					t.Fatal("expected artifact path")
				}
				if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(metadata.ArtifactPath))); err != nil {
					t.Fatalf("expected artifact: %v", err)
				}
			} else if metadata.ArtifactPath != "" {
				t.Fatalf("unexpected artifact path %q", metadata.ArtifactPath)
			}
		})
	}
}

func TestPackageTamperFailsVerification(t *testing.T) {
	t.Parallel()
	output := createAvailablePackage(t)
	path := filepath.Join(output, "derived", "synthetic-result.json")
	if err := os.WriteFile(path, []byte("{\"tampered\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := integrity.Verify(output); err == nil {
		t.Fatal("Verify() unexpectedly accepted tampered package")
	}
}

func TestMissingFileFailsVerification(t *testing.T) {
	t.Parallel()
	output := createAvailablePackage(t)
	if err := os.Remove(filepath.Join(output, "reports", "summary.txt")); err != nil {
		t.Fatal(err)
	}
	if err := integrity.Verify(output); err == nil {
		t.Fatal("Verify() unexpectedly accepted a missing file")
	}
}

func TestOverwriteRefused(t *testing.T) {
	t.Parallel()
	output := createAvailablePackage(t)
	if _, err := application.Collect(context.Background(), "available-collected", output); err == nil || !strings.Contains(err.Error(), "overwrite is refused") {
		t.Fatalf("second Collect() error = %v, want overwrite refusal", err)
	}
}

func TestOutputTraversalRefused(t *testing.T) {
	t.Parallel()
	output := filepath.Join(t.TempDir(), "safe") + string(filepath.Separator) + ".." + string(filepath.Separator) + "escape"
	if err := evidence.ValidateOutputPath(output); err == nil {
		t.Fatal("ValidateOutputPath() unexpectedly accepted traversal")
	}
}

func TestDriveRelativeTraversalRefused(t *testing.T) {
	t.Parallel()
	if err := evidence.ValidateOutputPath(`C:..\escape`); err == nil {
		t.Fatal("ValidateOutputPath() unexpectedly accepted drive-relative traversal")
	}
}

func createAvailablePackage(t *testing.T) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "package")
	if _, err := application.Collect(context.Background(), "available-collected", output); err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	return output
}

func readRecord(t *testing.T, root string) receipt.Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "meta", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record receipt.Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func assertPackageLayout(t *testing.T, root string) {
	t.Helper()
	for _, directory := range []string{"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports"} {
		info, err := os.Stat(filepath.Join(root, directory))
		if err != nil || !info.IsDir() {
			t.Fatalf("required directory %s missing or invalid: %v", directory, err)
		}
	}
}
