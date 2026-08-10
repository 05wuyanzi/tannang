// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package provider_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"github.com/05wuyanzi/tannang/internal/resolver"
)

type processIdentityAcceptanceRecord struct {
	ProcessID       uint32 `json:"process_id"`
	ParentProcessID uint32 `json:"parent_process_id"`
	ExecutableName  string `json:"executable_name"`
}

func TestWindowsProcessIdentitySnapshotAcceptance(t *testing.T) {
	if os.Getenv("TANNANG_RUN_WINDOWS_PROCESS_SNAPSHOT_ACCEPTANCE") != "1" {
		t.Skip("set TANNANG_RUN_WINDOWS_PROCESS_SNAPSHOT_ACCEPTANCE=1 for the separate benign Windows Acceptance Gate")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outputPath := filepath.Join(t.TempDir(), "future-evidence-package")
	target, err := fingerprint.Probe(ctx, outputPath, fingerprint.Options{})
	if err != nil {
		t.Fatalf("Probe() error: %v", err)
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("real TargetFingerprint.Validate() error: %v", err)
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		t.Skipf("PROCESS_IDENTITY_SNAPSHOT v0 does not support target architecture %q", target.Architecture)
	}

	runner := provider.NewProcessIdentitySnapshotRunner()
	descriptor := runner.Descriptor()
	if !descriptor.Requirements.Available {
		t.Fatalf("real Provider API unavailable: %s", descriptor.Requirements.AvailabilityReason)
	}
	decision, err := resolver.Resolve(
		capability.ProcessIdentitySnapshot(),
		target,
		[]provider.Descriptor{descriptor},
		resolver.Policy{},
	)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if err := resolver.ValidateDecision(decision); err != nil {
		t.Fatalf("ValidateDecision() error: %v", err)
	}
	if decision.Selected == nil || decision.Selected.ID != descriptor.ID ||
		decision.Compatibility != execution.Available || decision.Reason != execution.ReasonNone {
		t.Fatalf("unexpected real Provider selection: %+v", decision)
	}

	// A newly allocated bytes.Buffer is fresh, empty, exclusive, single-use,
	// uncommitted, and wholly discardable for this one acceptance invocation.
	var sink bytes.Buffer
	started := time.Now()
	result := runner.ExecuteTo(ctx, capability.ProcessIdentitySnapshot(), target, &sink)
	elapsed := time.Since(started)
	if result.State != execution.Collected || result.Reason != execution.ReasonNone {
		t.Fatalf("ExecuteTo() result = %+v, want COLLECTED/NONE", result)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("execution Result.Validate() error: %v", err)
	}
	if len(result.Payload) != 0 {
		t.Fatalf("real Provider returned synthetic Payload: %s", result.Payload)
	}
	if sink.Len() == 0 {
		t.Fatal("normal live Windows snapshot produced an empty artifact")
	}
	if elapsed > 30*time.Second {
		t.Fatalf("snapshot exceeded 30-second safety bound: %s", elapsed)
	}

	records := parseProcessIdentityAcceptanceRows(t, sink.Bytes())
	seen := make(map[uint32]struct{}, len(records))
	currentPID := uint32(os.Getpid())
	currentSeen := false
	for index, record := range records {
		if record.ExecutableName == "" {
			t.Fatalf("record[%d] has an empty executable name", index)
		}
		if _, duplicate := seen[record.ProcessID]; duplicate {
			t.Fatalf("duplicate process_id %d", record.ProcessID)
		}
		seen[record.ProcessID] = struct{}{}
		if record.ProcessID == currentPID {
			currentSeen = true
		}
	}
	if !currentSeen {
		t.Fatalf("current Tannang test process PID %d was not visible in its snapshot", currentPID)
	}

	firstHash := sha256.Sum256(sink.Bytes())
	secondHasher := sha256.New()
	if _, err := secondHasher.Write(sink.Bytes()); err != nil {
		t.Fatalf("independent SHA-256 write failed: %v", err)
	}
	secondHash := secondHasher.Sum(nil)
	if !bytes.Equal(firstHash[:], secondHash) {
		t.Fatalf("independent SHA-256 calculations disagree: %x != %x", firstHash, secondHash)
	}
	t.Logf("records=%d bytes=%d elapsed=%s sha256=%x target_architecture=%s", len(records), sink.Len(), elapsed, firstHash, target.Architecture)
}

func parseProcessIdentityAcceptanceRows(t *testing.T, data []byte) []processIdentityAcceptanceRecord {
	t.Helper()
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("acceptance artifact is empty or does not end on a complete NDJSON row")
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	records := make([]processIdentityAcceptanceRecord, 0, len(lines))
	for index, line := range lines {
		if len(line) == 0 {
			t.Fatalf("NDJSON row %d is empty", index)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			t.Fatalf("NDJSON row %d is invalid JSON: %v", index, err)
		}
		if len(fields) != 3 || fields["process_id"] == nil || fields["parent_process_id"] == nil || fields["executable_name"] == nil {
			t.Fatalf("NDJSON row %d does not match the exact record field set: %s", index, line)
		}
		var record processIdentityAcceptanceRecord
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("NDJSON row %d does not match the record schema: %v", index, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			t.Fatalf("NDJSON row %d contains trailing JSON: %v", index, err)
		}
		if strings.ContainsRune(record.ExecutableName, '\x00') {
			t.Fatalf("NDJSON row %d contains an embedded NUL executable name", index)
		}
		records = append(records, record)
	}
	return records
}
