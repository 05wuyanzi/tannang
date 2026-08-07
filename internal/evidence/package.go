// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package evidence creates an atomic, self-verifying synthetic package.
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

var directoryLayout = []string{
	"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports",
}

// Create writes a package to a temporary sibling and renames it only after
// every file and the integrity manifest have been written successfully.
func Create(output string, record receipt.Record, payload json.RawMessage) error {
	if err := ValidateOutputPath(output); err != nil {
		return err
	}
	if _, err := os.Lstat(output); err == nil {
		return errors.New("output package already exists; overwrite is refused")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output package: %w", err)
	}

	absOutput, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output package path: %w", err)
	}
	parent := filepath.Dir(absOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".tannang-package-")
	if err != nil {
		return fmt.Errorf("create temporary package: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()

	for _, directory := range directoryLayout {
		if err := os.Mkdir(filepath.Join(temporary, directory), 0o755); err != nil {
			return fmt.Errorf("create package directory %s: %w", directory, err)
		}
	}
	record.DirectoryLayout = append([]string(nil), directoryLayout...)
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate evidence receipt: %w", err)
	}
	if err := writeJSON(filepath.Join(temporary, "meta", "package.json"), record); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(temporary, "receipts", "execution.json"), record); err != nil {
		return err
	}

	if len(payload) > 0 && (record.Execution.State == execution.Collected || record.Execution.State == execution.Partial) {
		artifactPath := filepath.Join(temporary, filepath.FromSlash(record.ArtifactPath))
		if err := writePayloadJSON(artifactPath, payload); err != nil {
			return err
		}
		normalized := struct {
			SchemaVersion string          `json:"schema_version"`
			Source        string          `json:"source"`
			Payload       json.RawMessage `json:"payload"`
		}{SchemaVersion: "1.0", Source: record.ArtifactPath, Payload: payload}
		if err := writeJSON(filepath.Join(temporary, "normalized", "result.json"), normalized); err != nil {
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
	if err := writeJSON(filepath.Join(temporary, "handoff", "status.json"), handoff); err != nil {
		return err
	}
	report := fmt.Sprintf(
		"Tannang synthetic collection\n\nFixture: %s\nCompatibility: %s\nExecution: %s\nReason: %s\nProduction ready: false\n",
		record.FixtureName,
		record.Compatibility,
		record.Execution.State,
		record.Reason,
	)
	if err := os.WriteFile(filepath.Join(temporary, "reports", "summary.txt"), []byte(report), 0o644); err != nil {
		return fmt.Errorf("write package report: %w", err)
	}
	if err := integrity.Generate(temporary); err != nil {
		return err
	}
	if err := integrity.Verify(temporary); err != nil {
		return fmt.Errorf("verify package before commit: %w", err)
	}
	if err := os.Rename(temporary, absOutput); err != nil {
		return fmt.Errorf("commit evidence package: %w", err)
	}
	committed = true
	return nil
}

// ValidateOutputPath rejects omitted paths and obvious traversal components.
func ValidateOutputPath(output string) error {
	if strings.TrimSpace(output) == "" {
		return errors.New("output package path must be explicitly specified")
	}
	pathWithoutVolume := strings.TrimPrefix(output, filepath.VolumeName(output))
	parts := strings.FieldsFunc(pathWithoutVolume, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if part == ".." {
			return errors.New("output package path traversal is refused")
		}
	}
	cleaned := filepath.Clean(output)
	if cleaned == "." || cleaned == string(filepath.Separator) || filepath.Base(cleaned) == "." {
		return errors.New("output package path must name a new directory")
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal package JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write package JSON %s: %w", filepath.Base(path), err)
	}
	return nil
}

func writePayloadJSON(path string, value json.RawMessage) error {
	if !json.Valid(value) {
		return errors.New("synthetic payload is not valid JSON")
	}
	data := append([]byte(nil), value...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write synthetic payload: %w", err)
	}
	return nil
}
