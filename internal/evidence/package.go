// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package evidence creates guarded self-verifying evidence packages for the
// existing synthetic lane and the narrow FirstStage real-provider lane.
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

var directoryLayout = []string{
	"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports",
}

// Create builds in a guarded temporary sibling and publishes only after verify.
func Create(output string, record receipt.Record, payload json.RawMessage) (returnErr error) {
	root, err := pathsafe.CreateTemporarySibling(output)
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := root.Cleanup(); cleanupErr != nil {
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()

	for _, directory := range directoryLayout {
		if err := root.Mkdir(directory, 0o755); err != nil {
			return fmt.Errorf("create package directory %s: %w", directory, err)
		}
	}
	record.DirectoryLayout = append([]string(nil), directoryLayout...)
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate evidence receipt: %w", err)
	}
	if err := writeJSON(root, "meta/package.json", record); err != nil {
		return err
	}
	if err := writeJSON(root, "receipts/execution.json", record); err != nil {
		return err
	}

	if len(payload) > 0 && (record.Execution.State == execution.Collected || record.Execution.State == execution.Partial) {
		if err := writePayloadJSON(root, record.ArtifactPath, payload); err != nil {
			return err
		}
		normalized := struct {
			SchemaVersion string          `json:"schema_version"`
			Source        string          `json:"source"`
			Payload       json.RawMessage `json:"payload"`
		}{SchemaVersion: "1.0", Source: record.ArtifactPath, Payload: payload}
		if err := writeJSON(root, "normalized/result.json", normalized); err != nil {
			return err
		}
	}

	handoff := struct {
		SchemaVersion string `json:"schema_version"`
		Prepared      bool   `json:"prepared"`
		Executed      bool   `json:"executed"`
		Reason        string `json:"reason"`
	}{
		SchemaVersion: "1.0",
		Prepared:      false,
		Executed:      false,
		Reason:        "Synthetic genesis does not prepare or execute downstream handoffs.",
	}
	if err := writeJSON(root, "handoff/status.json", handoff); err != nil {
		return err
	}
	report := fmt.Sprintf(
		"Tannang synthetic collection\n\nFixture: %s\nCompatibility: %s\nExecution: %s\nReason: %s\nProduction ready: false\n",
		record.FixtureName,
		record.Compatibility,
		record.Execution.State,
		record.Reason,
	)
	if err := root.WriteFile("reports/summary.txt", []byte(report), 0o644); err != nil {
		return fmt.Errorf("write package report: %w", err)
	}
	if err := integrity.Generate(root.Path()); err != nil {
		return err
	}
	if err := integrity.Verify(root.Path()); err != nil {
		return fmt.Errorf("verify package before completion: %w", err)
	}
	if err := root.Publish(); err != nil {
		return fmt.Errorf("publish evidence package: %w", err)
	}
	return nil
}

// ValidateOutputPath validates the complete output-root safety policy.
func ValidateOutputPath(output string) error {
	return pathsafe.ValidateOutputPath(output)
}

func writeJSON(root *pathsafe.OutputRoot, relative string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal package JSON: %w", err)
	}
	data = append(data, '\n')
	if err := root.WriteFile(relative, data, 0o644); err != nil {
		return fmt.Errorf("write package JSON %s: %w", filepath.Base(relative), err)
	}
	return nil
}

func writePayloadJSON(root *pathsafe.OutputRoot, relative string, value json.RawMessage) error {
	if !json.Valid(value) {
		return errors.New("synthetic payload is not valid JSON")
	}
	data := append([]byte(nil), value...)
	if err := root.WriteFile(relative, data, 0o644); err != nil {
		return fmt.Errorf("write synthetic payload: %w", err)
	}
	return nil
}
