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

// RequestPriority provides a small deterministic ordering hint without
// encoding provider or resolver policy.
type RequestPriority string

const (
	PriorityEarly  RequestPriority = "EARLY"
	PriorityNormal RequestPriority = "NORMAL"
	PriorityLate   RequestPriority = "LATE"
)

// Capability describes requested evidence without binding it to a provider.
type Capability struct {
	ID                   string               `json:"id"`
	Description          string               `json:"description"`
	AcquisitionSemantics AcquisitionSemantics `json:"acquisition_semantics"`
	Sensitivity          string               `json:"sensitivity"`
}

// CapabilityRequest identifies requested evidence independently of a
// provider. Protected membership is trusted configuration interpreted by the
// application orchestration layer.
type CapabilityRequest struct {
	ID        string          `json:"id"`
	Priority  RequestPriority `json:"priority"`
	Protected bool            `json:"protected"`
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

// Valid reports whether the request priority is part of the v0 orchestration
// contract.
func (p RequestPriority) Valid() bool {
	switch p {
	case PriorityEarly, PriorityNormal, PriorityLate:
		return true
	default:
		return false
	}
}

// Order returns the stable sort order for a valid request priority.
func (p RequestPriority) Order() int {
	switch p {
	case PriorityEarly:
		return 0
	case PriorityNormal:
		return 1
	case PriorityLate:
		return 2
	default:
		return 3
	}
}

// Validate rejects malformed request identity and priority. It deliberately
// leaves protected-baseline ownership to the application layer.
func (r CapabilityRequest) Validate() error {
	if !ValidID(r.ID) {
		return errors.New("capability request id must contain only uppercase letters, digits, and underscores")
	}
	if !r.Priority.Valid() {
		return fmt.Errorf("unsupported capability request priority %q", r.Priority)
	}
	return nil
}

// Validate rejects malformed capability definitions.
func (c Capability) Validate() error {
	if !ValidID(c.ID) {
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

// ValidID reports whether value follows the committed Capability ID syntax.
// Application request handling reuses this authority rather than duplicating
// the syntax.
func ValidID(value string) bool {
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
