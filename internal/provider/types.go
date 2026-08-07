// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

// Class separates provider ownership and execution boundaries.
type Class string

const (
	WindowsInbox     Class = "WINDOWS_INBOX"
	FirstPartyNative Class = "FIRST_PARTY_NATIVE"
	ExternalBackend  Class = "EXTERNAL_BACKEND"
	SyntheticTest    Class = "SYNTHETIC_TEST"
)

// Requirements are declarative inputs to compatibility evaluation.
type Requirements struct {
	Platforms          []string         `json:"platforms"`
	OSFamilies         []string         `json:"os_families"`
	Architectures      []string         `json:"architectures"`
	RuntimeLanes       []string         `json:"runtime_lanes"`
	RequiresElevation  bool             `json:"requires_elevation"`
	Available          bool             `json:"available"`
	AvailabilityReason execution.Reason `json:"availability_reason"`
}

// Quality records selection dimensions without encoding provider-class order.
type Quality struct {
	Compatibility   execution.CompatibilityState `json:"compatibility"`
	Reason          execution.Reason             `json:"reason"`
	Fidelity        int                          `json:"fidelity"`
	Disturbance     int                          `json:"disturbance"`
	Completeness    int                          `json:"completeness"`
	OutputStability int                          `json:"output_stability"`
	EvidenceValue   int                          `json:"evidence_value"`
}

// Descriptor is the resolver-facing provider contract.
type Descriptor struct {
	ID           string       `json:"id"`
	Class        Class        `json:"class"`
	Capabilities []string     `json:"capabilities"`
	Requirements Requirements `json:"requirements"`
	SideEffects  []string     `json:"side_effects"`
	Quality      Quality      `json:"quality"`
}

// Runner executes exactly one selected provider.
type Runner interface {
	Descriptor() Descriptor
	Execute(context.Context, capability.Capability, fingerprint.TargetFingerprint) execution.Result
}

// Valid reports whether a class is recognized.
func (c Class) Valid() bool {
	switch c {
	case WindowsInbox, FirstPartyNative, ExternalBackend, SyntheticTest:
		return true
	default:
		return false
	}
}

// Supports reports whether the provider declares a capability.
func (d Descriptor) Supports(id string) bool {
	for _, candidate := range d.Capabilities {
		if candidate == id {
			return true
		}
	}
	return false
}

// Validate checks the declaration without probing the host.
func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return errors.New("provider id is required")
	}
	if !d.Class.Valid() {
		return fmt.Errorf("invalid provider class %q", d.Class)
	}
	if len(d.Capabilities) == 0 {
		return errors.New("provider capabilities are required")
	}
	if !d.Requirements.AvailabilityReason.Valid() {
		return errors.New("provider availability reason is invalid")
	}
	if !d.Quality.Compatibility.Valid() {
		return errors.New("provider quality compatibility is invalid")
	}
	if !d.Quality.Reason.Valid() {
		return errors.New("provider quality reason is invalid")
	}
	for name, value := range map[string]int{
		"fidelity":         d.Quality.Fidelity,
		"disturbance":      d.Quality.Disturbance,
		"completeness":     d.Quality.Completeness,
		"output_stability": d.Quality.OutputStability,
		"evidence_value":   d.Quality.EvidenceValue,
	} {
		if value < 0 || value > 5 {
			return fmt.Errorf("%s must be between 0 and 5", name)
		}
	}
	return nil
}
