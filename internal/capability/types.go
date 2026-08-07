// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package capability

import (
	"errors"
	"fmt"
	"strings"
)

// AcquisitionSemantics identifies how an artifact or observation is produced.
type AcquisitionSemantics string

const (
	ExistingArtifactExport  AcquisitionSemantics = "EXISTING_ARTIFACT_EXPORT"
	StateSnapshot           AcquisitionSemantics = "STATE_SNAPSHOT"
	DerivedDiagnosticReport AcquisitionSemantics = "DERIVED_DIAGNOSTIC_REPORT"
	ActiveTrace             AcquisitionSemantics = "ACTIVE_TRACE"
)

// Capability describes requested evidence without binding it to a provider.
type Capability struct {
	ID                   string               `json:"id"`
	Description          string               `json:"description"`
	AcquisitionSemantics AcquisitionSemantics `json:"acquisition_semantics"`
	Sensitivity          string               `json:"sensitivity"`
}

// Valid reports whether the acquisition semantic is part of the v0 contract.
func (s AcquisitionSemantics) Valid() bool {
	switch s {
	case ExistingArtifactExport, StateSnapshot, DerivedDiagnosticReport, ActiveTrace:
		return true
	default:
		return false
	}
}

// Validate rejects malformed capability definitions.
func (c Capability) Validate() error {
	if !validID(c.ID) {
		return errors.New("capability id must contain only uppercase letters, digits, and underscores")
	}
	if strings.TrimSpace(c.Description) == "" {
		return errors.New("capability description is required")
	}
	if !c.AcquisitionSemantics.Valid() {
		return fmt.Errorf("unsupported acquisition semantics %q", c.AcquisitionSemantics)
	}
	if strings.TrimSpace(c.Sensitivity) == "" {
		return errors.New("capability sensitivity is required")
	}
	return nil
}

func validID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}
