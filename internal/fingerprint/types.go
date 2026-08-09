// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package fingerprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// FieldState describes one fingerprint probe field without reusing provider
// compatibility or execution states.
type FieldState string

const (
	Known       FieldState = "KNOWN"
	Unavailable FieldState = "UNAVAILABLE"
	Failed      FieldState = "FAILED"
	Unsupported FieldState = "UNSUPPORTED"
)

// Field keeps a raw value separate from its probe state and provenance.
// Unknown fields never carry a value that could be mistaken for a measurement.
type Field[T any] struct {
	State       FieldState `json:"state"`
	Value       *T         `json:"value,omitempty"`
	Source      string     `json:"source"`
	CapturedAt  string     `json:"captured_at"`
	ErrorReason string     `json:"error_reason,omitempty"`
	ErrorCode   uint32     `json:"error_code,omitempty"`
}

// Valid reports whether a field state is part of the v0 probe contract.
func (s FieldState) Valid() bool {
	switch s {
	case Known, Unavailable, Failed, Unsupported:
		return true
	default:
		return false
	}
}

// Validate enforces value/status/provenance separation.
func (f Field[T]) Validate(name string) error {
	if !f.State.Valid() {
		return fmt.Errorf("%s has invalid field state %q", name, f.State)
	}
	if strings.TrimSpace(f.Source) == "" {
		return fmt.Errorf("%s source is required", name)
	}
	if strings.TrimSpace(f.CapturedAt) == "" {
		return fmt.Errorf("%s captured_at is required", name)
	}
	if _, err := time.Parse(time.RFC3339Nano, f.CapturedAt); err != nil {
		return fmt.Errorf("%s captured_at is invalid: %w", name, err)
	}
	if f.State == Known {
		if f.Value == nil {
			return fmt.Errorf("%s KNOWN field requires a value", name)
		}
		if value, isString := any(*f.Value).(string); isString && strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s KNOWN string field requires a non-whitespace value", name)
		}
		if f.ErrorReason != "" || f.ErrorCode != 0 {
			return fmt.Errorf("%s KNOWN field must not contain an error", name)
		}
		return nil
	}
	if f.Value != nil {
		return fmt.Errorf("%s non-KNOWN field must not contain a value", name)
	}
	if strings.TrimSpace(f.ErrorReason) == "" {
		return fmt.Errorf("%s non-KNOWN field requires an error reason", name)
	}
	return nil
}

// OutputVolumeFacts describe resources for the exact output path already
// accepted by PATHSAFE. They never authorize or select an output location.
type OutputVolumeFacts struct {
	ValidatedOutputPath  Field[string] `json:"validated_output_path"`
	VolumeRoot           Field[string] `json:"volume_root"`
	DriveType            Field[string] `json:"drive_type"`
	FileSystem           Field[string] `json:"filesystem"`
	AvailableBytesCaller Field[uint64] `json:"available_bytes_to_caller"`
}

// ProbeFields are the bounded raw facts collected by the real host probe.
// RuntimeLane remains a separate resolver/policy annotation.
type ProbeFields struct {
	OSVersion                    Field[string]     `json:"os_version"`
	OSBuild                      Field[string]     `json:"os_build"`
	NativeArchitecture           Field[string]     `json:"native_architecture"`
	ProcessArchitecture          Field[string]     `json:"process_architecture"`
	LogicalProcessorCount        Field[uint32]     `json:"logical_processor_count"`
	TotalPhysicalMemoryBytes     Field[uint64]     `json:"total_physical_memory_bytes"`
	AvailablePhysicalMemoryBytes Field[uint64]     `json:"available_physical_memory_bytes"`
	Elevated                     Field[bool]       `json:"elevated"`
	TokenElevationType           Field[string]     `json:"token_elevation_type"`
	OutputVolume                 OutputVolumeFacts `json:"output_volume"`
	CPUBusyBasisPoints           *Field[uint32]    `json:"cpu_busy_basis_points,omitempty"`
}

// TargetFingerprint remains compatible with trusted synthetic fixtures while
// optionally carrying bounded real probe facts. A native probe never assigns a
// runtime lane or other provider-selection policy.
type TargetFingerprint struct {
	Platform     string       `json:"platform"`
	OSFamily     string       `json:"os_family"`
	Version      string       `json:"version"`
	Build        string       `json:"build"`
	Architecture string       `json:"architecture"`
	Privilege    string       `json:"privilege"`
	Elevated     bool         `json:"elevated"`
	RuntimeLane  string       `json:"runtime_lane,omitempty"`
	Probe        *ProbeFields `json:"probe,omitempty"`
}

// Clone returns a deep copy suitable for mutable resolver, provider, and
// finalizer boundaries. The retained run snapshot therefore cannot be changed
// through Field values or optional probe pointers shared with a consumer.
func (f TargetFingerprint) Clone() TargetFingerprint {
	clone := f
	if f.Probe == nil {
		return clone
	}
	probe := *f.Probe
	probe.OSVersion = cloneField(f.Probe.OSVersion)
	probe.OSBuild = cloneField(f.Probe.OSBuild)
	probe.NativeArchitecture = cloneField(f.Probe.NativeArchitecture)
	probe.ProcessArchitecture = cloneField(f.Probe.ProcessArchitecture)
	probe.LogicalProcessorCount = cloneField(f.Probe.LogicalProcessorCount)
	probe.TotalPhysicalMemoryBytes = cloneField(f.Probe.TotalPhysicalMemoryBytes)
	probe.AvailablePhysicalMemoryBytes = cloneField(f.Probe.AvailablePhysicalMemoryBytes)
	probe.Elevated = cloneField(f.Probe.Elevated)
	probe.TokenElevationType = cloneField(f.Probe.TokenElevationType)
	probe.OutputVolume.ValidatedOutputPath = cloneField(f.Probe.OutputVolume.ValidatedOutputPath)
	probe.OutputVolume.VolumeRoot = cloneField(f.Probe.OutputVolume.VolumeRoot)
	probe.OutputVolume.DriveType = cloneField(f.Probe.OutputVolume.DriveType)
	probe.OutputVolume.FileSystem = cloneField(f.Probe.OutputVolume.FileSystem)
	probe.OutputVolume.AvailableBytesCaller = cloneField(f.Probe.OutputVolume.AvailableBytesCaller)
	if f.Probe.CPUBusyBasisPoints != nil {
		cpu := cloneField(*f.Probe.CPUBusyBasisPoints)
		probe.CPUBusyBasisPoints = &cpu
	}
	clone.Probe = &probe
	return clone
}

func cloneField[T any](field Field[T]) Field[T] {
	clone := field
	if field.Value != nil {
		value := *field.Value
		clone.Value = &value
	}
	return clone
}

// MarshalJSON keeps trusted synthetic fixtures byte-contract compatible while
// omitting unresolved compatibility mirrors from partial real probe results.
// Field-level probe state remains the authority until Validate succeeds.
func (f TargetFingerprint) MarshalJSON() ([]byte, error) {
	type wireTarget struct {
		Platform     string       `json:"platform"`
		OSFamily     string       `json:"os_family,omitempty"`
		Version      string       `json:"version,omitempty"`
		Build        string       `json:"build,omitempty"`
		Architecture string       `json:"architecture,omitempty"`
		Privilege    string       `json:"privilege,omitempty"`
		Elevated     *bool        `json:"elevated,omitempty"`
		RuntimeLane  string       `json:"runtime_lane,omitempty"`
		Probe        *ProbeFields `json:"probe,omitempty"`
	}
	var elevated *bool
	if f.Probe == nil || f.Probe.Elevated.State == Known {
		value := f.Elevated
		elevated = &value
	}
	return json.Marshal(wireTarget{
		Platform:     f.Platform,
		OSFamily:     f.OSFamily,
		Version:      f.Version,
		Build:        f.Build,
		Architecture: f.Architecture,
		Privilege:    f.Privilege,
		Elevated:     elevated,
		RuntimeLane:  f.RuntimeLane,
		Probe:        f.Probe,
	})
}

// Validate checks contract completeness and fails closed when a real probe did
// not establish the hard resolver inputs. It performs no host probes.
func (f TargetFingerprint) Validate() error {
	values := map[string]string{
		"platform":     f.Platform,
		"os_family":    f.OSFamily,
		"version":      f.Version,
		"build":        f.Build,
		"architecture": f.Architecture,
	}
	if f.Probe == nil {
		values["privilege"] = f.Privilege
		values["runtime_lane"] = f.RuntimeLane
	}
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
	}
	if f.Probe == nil {
		return nil
	}
	return f.validateProbe()
}

func (f TargetFingerprint) validateProbe() error {
	p := f.Probe
	fields := []struct {
		name  string
		check func() error
	}{
		{"probe.os_version", func() error { return p.OSVersion.Validate("probe.os_version") }},
		{"probe.os_build", func() error { return p.OSBuild.Validate("probe.os_build") }},
		{"probe.native_architecture", func() error { return p.NativeArchitecture.Validate("probe.native_architecture") }},
		{"probe.process_architecture", func() error { return p.ProcessArchitecture.Validate("probe.process_architecture") }},
		{"probe.logical_processor_count", func() error { return p.LogicalProcessorCount.Validate("probe.logical_processor_count") }},
		{"probe.total_physical_memory_bytes", func() error { return p.TotalPhysicalMemoryBytes.Validate("probe.total_physical_memory_bytes") }},
		{"probe.available_physical_memory_bytes", func() error { return p.AvailablePhysicalMemoryBytes.Validate("probe.available_physical_memory_bytes") }},
		{"probe.elevated", func() error { return p.Elevated.Validate("probe.elevated") }},
		{"probe.token_elevation_type", func() error { return p.TokenElevationType.Validate("probe.token_elevation_type") }},
		{"probe.output_volume.validated_output_path", func() error {
			return p.OutputVolume.ValidatedOutputPath.Validate("probe.output_volume.validated_output_path")
		}},
		{"probe.output_volume.volume_root", func() error { return p.OutputVolume.VolumeRoot.Validate("probe.output_volume.volume_root") }},
		{"probe.output_volume.drive_type", func() error { return p.OutputVolume.DriveType.Validate("probe.output_volume.drive_type") }},
		{"probe.output_volume.filesystem", func() error { return p.OutputVolume.FileSystem.Validate("probe.output_volume.filesystem") }},
		{"probe.output_volume.available_bytes_to_caller", func() error {
			return p.OutputVolume.AvailableBytesCaller.Validate("probe.output_volume.available_bytes_to_caller")
		}},
	}
	for _, field := range fields {
		if err := field.check(); err != nil {
			return err
		}
	}
	if p.CPUBusyBasisPoints != nil {
		if err := p.CPUBusyBasisPoints.Validate("probe.cpu_busy_basis_points"); err != nil {
			return err
		}
		if p.CPUBusyBasisPoints.State == Known && *p.CPUBusyBasisPoints.Value > 10000 {
			return errors.New("probe.cpu_busy_basis_points exceeds 10000")
		}
	}

	// These fields are hard resolver inputs. Their explicit probe values must
	// agree with the compatibility-facing fields.
	if p.OSVersion.State != Known || p.OSBuild.State != Known || p.NativeArchitecture.State != Known ||
		p.ProcessArchitecture.State != Known {
		return errors.New("real fingerprint is not resolver-ready because a hard probe field is unknown")
	}
	if *p.OSVersion.Value != f.Version || *p.OSBuild.Value != f.Build || *p.NativeArchitecture.Value != f.Architecture {
		return errors.New("real fingerprint compatibility fields disagree with probe values")
	}
	if p.Elevated.State == Known && *p.Elevated.Value != f.Elevated {
		return errors.New("real fingerprint elevated mirror disagrees with probe value")
	}
	if p.TokenElevationType.State == Known {
		if !knownTokenElevationType(*p.TokenElevationType.Value) {
			return errors.New("probe.token_elevation_type has an invalid value")
		}
		if p.Elevated.State == Known && strings.TrimSpace(f.Privilege) != "" {
			if f.Privilege != privilegeLabelFromProbe(*p.Elevated.Value, *p.TokenElevationType.Value) {
				return errors.New("real fingerprint privilege mirror disagrees with probe values")
			}
		}
	}
	if strings.TrimSpace(*p.ProcessArchitecture.Value) == "" {
		return errors.New("probe.process_architecture value is required")
	}
	if p.LogicalProcessorCount.State == Known && *p.LogicalProcessorCount.Value == 0 {
		return errors.New("probe.logical_processor_count must be positive")
	}
	if p.TotalPhysicalMemoryBytes.State == Known && *p.TotalPhysicalMemoryBytes.Value == 0 {
		return errors.New("probe.total_physical_memory_bytes must be positive")
	}
	if p.TotalPhysicalMemoryBytes.State == Known && p.AvailablePhysicalMemoryBytes.State == Known &&
		*p.AvailablePhysicalMemoryBytes.Value > *p.TotalPhysicalMemoryBytes.Value {
		return errors.New("available physical memory exceeds total physical memory")
	}
	if p.OutputVolume.ValidatedOutputPath.State != Known || p.OutputVolume.VolumeRoot.State != Known {
		return errors.New("real fingerprint is not associated with a validated output path")
	}
	return nil
}

func knownTokenElevationType(value string) bool {
	switch value {
	case "default", "full", "limited":
		return true
	default:
		return false
	}
}

func privilegeLabelFromProbe(elevated bool, elevationType string) string {
	if elevated {
		return "elevated"
	}
	if elevationType == "limited" {
		return "filtered-admin"
	}
	return "standard-user"
}
