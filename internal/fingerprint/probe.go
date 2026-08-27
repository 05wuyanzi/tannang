// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package fingerprint defines resolver-facing target facts and a bounded,
// read-only host probe. The probe is compatibility context, not evidence
// acquisition.
package fingerprint

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const cpuPressureSampleInterval = 250 * time.Millisecond

var ErrUnsupportedPlatform = errors.New("target fingerprint probing is unsupported on this platform")

// Options controls only the optional bounded CPU sample.
type Options struct {
	IncludeCPUPressure bool
}

// Probe reads the minimum local target facts for future resolver decisions.
// outputPath must name a new output child accepted by the existing PATHSAFE
// contract. Probe creates no files or directories.
func Probe(ctx context.Context, outputPath string, options Options) (TargetFingerprint, error) {
	if ctx == nil {
		return TargetFingerprint{}, errors.New("fingerprint probe context is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return TargetFingerprint{}, errors.New("validated output path is required")
	}
	return platformProbe(ctx, outputPath, options)
}

func knownField[T any](value T, source string, capturedAt time.Time) Field[T] {
	copy := value
	return Field[T]{
		State:      Known,
		Value:      &copy,
		Source:     source,
		CapturedAt: capturedAt.UTC().Format(time.RFC3339Nano),
	}
}

func unavailableField[T any](source, reason string, capturedAt time.Time) Field[T] {
	return errorField[T](Unavailable, source, reason, 0, capturedAt)
}

func failedField[T any](source string, err error, capturedAt time.Time) Field[T] {
	reason := "API_CALL_FAILED"
	var probeErr *apiError
	if errors.As(err, &probeErr) {
		if probeErr.reason != "" {
			reason = probeErr.reason
		}
		return errorField[T](Failed, source, reason, probeErr.code, capturedAt)
	}
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		reason = "PROBE_FAILED"
	}
	return errorField[T](Failed, source, reason, 0, capturedAt)
}

func unsupportedField[T any](source, reason string, capturedAt time.Time) Field[T] {
	return errorField[T](Unsupported, source, reason, 0, capturedAt)
}

func errorField[T any](state FieldState, source, reason string, code uint32, capturedAt time.Time) Field[T] {
	return Field[T]{
		State:       state,
		Source:      source,
		CapturedAt:  capturedAt.UTC().Format(time.RFC3339Nano),
		ErrorReason: reason,
		ErrorCode:   code,
	}
}

type apiError struct {
	reason string
	code   uint32
}

func (e *apiError) Error() string {
	if e == nil {
		return "Windows API failure"
	}
	if e.code == 0 {
		return e.reason
	}
	return fmt.Sprintf("%s (code %d)", e.reason, e.code)
}
