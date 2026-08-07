// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	syntheticfixtures "github.com/05wuyanzi/tannang/synthetic-fixtures"
)

// SyntheticBehavior defines deterministic provider output for one fixture.
type SyntheticBehavior struct {
	ProviderID        string           `json:"provider_id"`
	ExecutionState    execution.State  `json:"execution_state"`
	Reason            execution.Reason `json:"reason"`
	Detail            string           `json:"detail"`
	RawOrDerived      string           `json:"raw_or_derived"`
	SideEffectSummary string           `json:"side_effect_summary"`
	Payload           json.RawMessage  `json:"payload,omitempty"`
}

// Fixture is an embedded synthetic end-to-end request.
type Fixture struct {
	SchemaVersion string                        `json:"schema_version"`
	Name          string                        `json:"name"`
	Capability    capability.Capability         `json:"capability"`
	Target        fingerprint.TargetFingerprint `json:"target_fingerprint"`
	Providers     []Descriptor                  `json:"providers"`
	Behavior      SyntheticBehavior             `json:"behavior"`
}

// LoadFixture reads a fixed, embedded fixture by simple name.
func LoadFixture(name string) (Fixture, error) {
	if !validFixtureName(name) {
		return Fixture{}, errors.New("synthetic fixture must be a simple embedded fixture name")
	}
	data, err := syntheticfixtures.Read(name)
	if err != nil {
		return Fixture{}, fmt.Errorf("read embedded synthetic fixture: %w", err)
	}

	var fixture Fixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return Fixture{}, fmt.Errorf("decode synthetic fixture: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Fixture{}, errors.New("synthetic fixture contains trailing JSON values")
		}
		return Fixture{}, fmt.Errorf("decode trailing synthetic fixture data: %w", err)
	}
	if err := fixture.Validate(name); err != nil {
		return Fixture{}, err
	}
	return fixture, nil
}

// Validate ensures an embedded fixture cannot cross into a production class.
func (f Fixture) Validate(requestedName string) error {
	if f.SchemaVersion != "1.0" {
		return errors.New("synthetic fixture schema_version must be 1.0")
	}
	if f.Name != requestedName {
		return errors.New("synthetic fixture name does not match the embedded file")
	}
	if err := f.Capability.Validate(); err != nil {
		return fmt.Errorf("validate fixture capability: %w", err)
	}
	if err := f.Target.Validate(); err != nil {
		return fmt.Errorf("validate fixture target: %w", err)
	}
	if len(f.Providers) == 0 {
		return errors.New("synthetic fixture requires at least one provider descriptor")
	}

	behaviorProviderFound := f.Behavior.ProviderID == ""
	for _, descriptor := range f.Providers {
		if err := descriptor.Validate(); err != nil {
			return fmt.Errorf("validate synthetic provider: %w", err)
		}
		if descriptor.Class != SyntheticTest {
			return errors.New("synthetic fixture may only declare SYNTHETIC_TEST providers")
		}
		if descriptor.ID == f.Behavior.ProviderID {
			behaviorProviderFound = true
		}
	}
	if !behaviorProviderFound {
		return errors.New("synthetic behavior provider is not declared")
	}
	if !f.Behavior.ExecutionState.Valid() {
		return errors.New("synthetic execution state is invalid")
	}
	if !f.Behavior.Reason.Valid() {
		return errors.New("synthetic execution reason is invalid")
	}
	if f.Behavior.RawOrDerived != "RAW" && f.Behavior.RawOrDerived != "DERIVED" {
		return errors.New("synthetic raw_or_derived must be RAW or DERIVED")
	}
	if strings.TrimSpace(f.Behavior.SideEffectSummary) == "" {
		return errors.New("synthetic side effect summary is required")
	}
	return nil
}

// SyntheticRunner executes no commands and reads no host evidence.
type SyntheticRunner struct {
	fixture    Fixture
	descriptor Descriptor
}

// NewSyntheticRunner binds the selected descriptor to its embedded behavior.
func NewSyntheticRunner(fixture Fixture, descriptor Descriptor) (*SyntheticRunner, error) {
	if descriptor.Class != SyntheticTest {
		return nil, errors.New("synthetic runner rejects non-test provider classes")
	}
	if fixture.Behavior.ProviderID != descriptor.ID {
		return nil, errors.New("selected provider does not match fixture behavior")
	}
	return &SyntheticRunner{fixture: fixture, descriptor: descriptor}, nil
}

// Descriptor returns the selected declaration.
func (r *SyntheticRunner) Descriptor() Descriptor {
	return r.descriptor
}

// Execute returns fixed fixture data only.
func (r *SyntheticRunner) Execute(
	ctx context.Context,
	request capability.Capability,
	target fingerprint.TargetFingerprint,
) execution.Result {
	if request.AcquisitionSemantics == capability.ActiveTrace {
		return execution.Result{
			State:  execution.Blocked,
			Reason: execution.ReasonPolicyDisabled,
			Detail: "ACTIVE_TRACE is not executable in the genesis synthetic provider",
		}
	}
	select {
	case <-ctx.Done():
		return execution.Result{
			State:  execution.Failed,
			Reason: execution.ReasonTimeout,
			Detail: "synthetic execution context ended",
		}
	default:
	}
	if r.descriptor.Requirements.RequiresElevation && !target.Elevated {
		return execution.Result{
			State:  execution.Blocked,
			Reason: execution.ReasonPrivilegeRequired,
			Detail: r.fixture.Behavior.Detail,
		}
	}
	return execution.Result{
		State:   r.fixture.Behavior.ExecutionState,
		Reason:  r.fixture.Behavior.Reason,
		Detail:  r.fixture.Behavior.Detail,
		Payload: append(json.RawMessage(nil), r.fixture.Behavior.Payload...),
	}
}

func validFixtureName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}
