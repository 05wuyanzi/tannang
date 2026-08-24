// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package evidence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

const firstStageHandoffReason = "FirstStage downstream handoff is not activated."

// FirstStagePackageSession owns one guarded temporary package for one real
// FirstStage Run. It is intentionally the only object allowed to retain or
// publish the artifact.
type FirstStagePackageSession struct {
	mu sync.Mutex

	root       *pathsafe.OutputRoot
	output     string
	beginError error

	openAttempted   bool
	artifact        *osFileAdapter
	sealed          bool
	retained        bool
	removeAttempted bool
	removed         bool
	sealError       error

	abortDone        bool
	abortError       error
	residualPossible bool
	published        bool
	multiArtifacts   map[string]*namedArtifactState
}

type namedArtifactState struct {
	relative        string
	mode            string
	file            *osFileAdapter
	reserved        bool
	sealed          bool
	retained        bool
	removeAttempted bool
	removed         bool
	sealError       error
}

// osFileAdapter keeps the application seam limited to io.Writer while the
// package implementation retains Sync/Close authority over the real handle.
type osFileAdapter struct {
	file interface {
		io.Writer
		Sync() error
		Close() error
	}
}

func (f *osFileAdapter) Write(data []byte) (int, error) {
	if f == nil || f.file == nil {
		return 0, errors.New("first-stage artifact file is closed")
	}
	return f.file.Write(data)
}

// BeginFirstStagePackage creates the unique temporary package and fixed empty
// directory layout. On a post-root failure it returns the partially-owned
// session together with the error so the caller can Abort that same session.
func BeginFirstStagePackage(ctx context.Context, output string) (*FirstStagePackageSession, error) {
	if ctx == nil {
		return nil, errors.New("first-stage package context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := pathsafe.CreateTemporarySibling(output)
	if err != nil {
		return nil, err
	}
	session := &FirstStagePackageSession{root: root, output: output}
	for _, directory := range directoryLayout {
		if err := ctx.Err(); err != nil {
			session.beginError = err
			return session, err
		}
		if err := root.Mkdir(directory, 0o755); err != nil {
			session.beginError = fmt.Errorf("create first-stage package directory %s: %w", directory, err)
			return session, session.beginError
		}
	}
	session.multiArtifacts = make(map[string]*namedArtifactState)
	return session, nil
}

// Output returns the caller-selected final package destination.
func (s *FirstStagePackageSession) Output() string {
	if s == nil {
		return ""
	}
	return s.output
}

// OpenArtifact creates the one fixed, fresh, exclusive streaming sink.
func (s *FirstStagePackageSession) OpenArtifact() (io.Writer, error) {
	if s == nil {
		return nil, errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openAttempted {
		return nil, errors.New("first-stage artifact Open was already attempted")
	}
	s.openAttempted = true
	if s.beginError != nil {
		return nil, s.beginError
	}
	file, err := s.root.CreateFile(receipt.FirstStageArtifactPath, 0o600)
	if err != nil {
		return nil, err
	}
	s.artifact = &osFileAdapter{file: file}
	return s.artifact, nil
}

func namedArtifactPath(capabilityID string) (string, string, error) {
	switch capabilityID {
	case capability.ProcessIdentitySnapshotID:
		return receipt.FirstStageArtifactPath, "stream", nil
	case capability.WindowsEventLogSystemChannelID:
		return receipt.WindowsEventLogSystemArtifactPath, "file", nil
	default:
		return "", "", errors.New("unsupported real artifact capability")
	}
}

// OpenStreamingArtifact creates a named streaming artifact for the known
// process capability.
func (s *FirstStagePackageSession) OpenStreamingArtifact(capabilityID string) (io.Writer, error) {
	if s == nil {
		return nil, errors.New("first-stage package session is nil")
	}
	relative, mode, err := namedArtifactPath(capabilityID)
	if err != nil || mode != "stream" {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.multiArtifacts == nil {
		s.multiArtifacts = make(map[string]*namedArtifactState)
	}
	if _, exists := s.multiArtifacts[capabilityID]; exists {
		return nil, errors.New("named artifact Open was already attempted")
	}
	file, err := s.root.CreateFile(relative, 0o600)
	if err != nil {
		return nil, err
	}
	state := &namedArtifactState{relative: relative, mode: mode, file: &osFileAdapter{file: file}}
	s.multiArtifacts[capabilityID] = state
	return state.file, nil
}

// ReserveFileArtifactPath validates a fresh target without pre-creating it.
func (s *FirstStagePackageSession) ReserveFileArtifactPath(capabilityID string) (string, error) {
	if s == nil {
		return "", errors.New("first-stage package session is nil")
	}
	relative, mode, err := namedArtifactPath(capabilityID)
	if err != nil || mode != "file" {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.multiArtifacts == nil {
		s.multiArtifacts = make(map[string]*namedArtifactState)
	}
	if _, exists := s.multiArtifacts[capabilityID]; exists {
		return "", errors.New("named artifact reservation was already attempted")
	}
	if s.beginError != nil {
		return "", s.beginError
	}
	full, err := s.root.ReserveFilePath(relative)
	if err != nil {
		return "", err
	}
	s.multiArtifacts[capabilityID] = &namedArtifactState{relative: relative, mode: mode, reserved: true}
	return full, nil
}

func (s *FirstStagePackageSession) ValidateFileArtifact(capabilityID string) error {
	if s == nil {
		return errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.multiArtifacts[capabilityID]
	if !ok || state.mode != "file" || !state.reserved || state.sealed {
		return errors.New("file artifact is not reserved")
	}
	return s.root.ValidateExistingFile(state.relative)
}

func (s *FirstStagePackageSession) SealNamedArtifact(capabilityID string, retain bool) error {
	if s == nil {
		return errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.multiArtifacts[capabilityID]
	if !ok {
		return errors.New("named artifact was not opened or reserved")
	}
	if state.sealed {
		if state.retained == retain {
			return state.sealError
		}
		return errors.New("named artifact disposition was already fixed")
	}
	state.sealed, state.retained = true, retain
	if state.mode == "stream" {
		var syncErr, closeErr error
		if retain && state.file != nil && state.file.file != nil {
			syncErr = state.file.file.Sync()
		}
		if state.file != nil && state.file.file != nil {
			closeErr = state.file.file.Close()
			if closeErr == nil {
				state.file.file = nil
			}
		}
		if !retain {
			state.removeAttempted = true
			removeErr := s.root.RemoveFile(state.relative)
			if removeErr == nil {
				state.removed = true
			}
			state.sealError = errors.Join(closeErr, removeErr)
			return state.sealError
		}
		state.sealError = errors.Join(syncErr, closeErr)
		return state.sealError
	}
	if retain {
		state.sealError = s.root.ValidateExistingFile(state.relative)
		return state.sealError
	}
	state.removeAttempted = true
	removeErr := s.root.RemoveFile(state.relative)
	if removeErr == nil {
		state.removed = true
	}
	state.sealError = removeErr
	return state.sealError
}

func (s *FirstStagePackageSession) HashNamedArtifact(capabilityID string) (integrity.Entry, error) {
	if s == nil {
		return integrity.Entry{}, errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.multiArtifacts[capabilityID]
	if !ok || !state.sealed || !state.retained || state.sealError != nil {
		return integrity.Entry{}, errors.New("named artifact is not a successfully retained file")
	}
	return integrity.HashFile(s.root.Path(), state.relative)
}

// SealArtifact applies the retain/discard state machine exactly once.
func (s *FirstStagePackageSession) SealArtifact(retain bool) error {
	if s == nil {
		return errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sealArtifactLocked(retain)
}

func (s *FirstStagePackageSession) sealArtifactLocked(retain bool) error {
	if s.sealed {
		if s.retained == retain {
			return s.sealError
		}
		return errors.New("first-stage artifact disposition was already fixed")
	}
	s.sealed = true
	s.retained = retain
	if s.artifact == nil {
		if retain {
			s.sealError = errors.New("cannot retain an unopened first-stage artifact")
		}
		return s.sealError
	}
	var syncErr error
	var closeErr error
	if retain && s.artifact.file != nil {
		syncErr = s.artifact.file.Sync()
	}
	if s.artifact.file != nil {
		closeErr = s.artifact.file.Close()
		if closeErr == nil {
			s.artifact.file = nil
		}
	}
	if !retain {
		s.removeAttempted = true
		removeErr := s.root.RemoveFile(receipt.FirstStageArtifactPath)
		if removeErr == nil {
			s.removed = true
		} else {
			s.residualPossible = true
		}
		s.sealError = errors.Join(closeErr, removeErr)
		return s.sealError
	}
	s.sealError = errors.Join(syncErr, closeErr)
	if s.sealError != nil {
		s.residualPossible = true
	}
	return s.sealError
}

// HashArtifact hashes the closed retained artifact through PATHSAFE.
func (s *FirstStagePackageSession) HashArtifact() (integrity.Entry, error) {
	if s == nil {
		return integrity.Entry{}, errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sealed || !s.retained || s.sealError != nil {
		return integrity.Entry{}, errors.New("first-stage artifact is not a successfully retained file")
	}
	return integrity.HashFile(s.root.Path(), receipt.FirstStageArtifactPath)
}

// Finalize writes metadata, receipts and handoff, verifies the complete tree,
// checks the artifact hash a second time, and publishes as the final mutation.
func (s *FirstStagePackageSession) Finalize(ctx context.Context, metadata receipt.FirstStagePackageMetadata, records []receipt.FirstStageRecord) error {
	if s == nil {
		return errors.New("first-stage package session is nil")
	}
	if ctx == nil {
		return errors.New("first-stage finalization context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.abortDone {
		return errors.New("first-stage package session was aborted")
	}
	if s.published {
		return errors.New("first-stage package session is already published")
	}
	if s.openAttempted && (!s.sealed || s.sealError != nil) {
		return errors.New("first-stage artifact disposition is incomplete")
	}
	for _, state := range s.multiArtifacts {
		if !state.sealed || state.sealError != nil {
			return errors.New("named artifact disposition is incomplete")
		}
	}
	metadata.DirectoryLayout = append([]string(nil), directoryLayout...)
	if err := metadata.Validate(); err != nil {
		return fmt.Errorf("validate first-stage package metadata: %w", err)
	}
	if len(records) != len(metadata.ReceiptReferences) {
		return errors.New("first-stage receipt count does not match metadata")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeJSON(s.root, "meta/package.json", metadata); err != nil {
		return err
	}
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("validate first-stage receipt %s: %w", record.RequestedCapability.ID, err)
		}
		if err := writeJSON(s.root, receipt.FirstStageReceiptPath(record.RequestedCapability.ID), record); err != nil {
			return err
		}
	}
	handoff := struct {
		SchemaVersion string `json:"schema_version"`
		Prepared      bool   `json:"prepared"`
		Executed      bool   `json:"executed"`
		Reason        string `json:"reason"`
	}{SchemaVersion: "1.0", Prepared: false, Executed: false, Reason: firstStageHandoffReason}
	if err := writeJSON(s.root, "handoff/status.json", handoff); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := integrity.Generate(s.root.Path()); err != nil {
		return err
	}
	if err := integrity.Verify(s.root.Path()); err != nil {
		return fmt.Errorf("verify first-stage package: %w", err)
	}
	for _, reference := range metadata.ArtifactReferences {
		entry, err := integrity.HashFile(s.root.Path(), reference.Path)
		if err != nil {
			return err
		}
		if entry.Size != reference.Size || entry.SHA256 != reference.SHA256 {
			return errors.New("first-stage artifact changed after receipt publication")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Publish(); err != nil {
		return fmt.Errorf("publish first-stage package: %w", err)
	}
	s.published = true
	return nil
}

// Abort executes one bounded cleanup sequence and caches its result forever.
func (s *FirstStagePackageSession) Abort() error {
	if s == nil {
		return errors.New("first-stage package session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.abortDone {
		return s.abortError
	}
	s.abortDone = true
	if s.published {
		return nil
	}
	var cleanupErr error
	for _, state := range s.multiArtifacts {
		if !state.sealed {
			if state.mode == "stream" && state.file != nil && state.file.file != nil {
				_ = state.file.file.Close()
				state.file.file = nil
			}
			if err := s.root.RemoveFile(state.relative); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			state.sealed = true
		}
	}
	if !s.sealed && s.artifact != nil {
		cleanupErr = s.sealArtifactLocked(false)
	}
	if s.removeAttempted && !s.removed {
		s.abortError = errors.Join(cleanupErr, s.sealError, errors.New("residual staging bytes may remain"))
		s.residualPossible = true
		return s.abortError
	}
	if cleanupErr == nil && s.sealError != nil {
		cleanupErr = s.sealError
	}
	rootErr := s.root.Cleanup()
	if rootErr != nil {
		s.residualPossible = true
	}
	s.abortError = errors.Join(cleanupErr, rootErr)
	return s.abortError
}

// ResidualStagingPossible reports the bounded cleanup truth for diagnostics.
func (s *FirstStagePackageSession) ResidualStagingPossible() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.residualPossible
}
