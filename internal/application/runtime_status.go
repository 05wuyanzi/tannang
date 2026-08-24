// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package application

import "fmt"

// RuntimeEvent is a bounded, non-evidence observation of first-stage work.
type RuntimeEvent struct {
	Type  string
	Event string
}

// RuntimeEventSink accepts a best-effort observation without blocking.
// False means the observation was dropped.
type RuntimeEventSink interface {
	TryEmit(RuntimeEvent) bool
}

const (
	RuntimeTypeStart      = "START"
	RuntimeTypePhase      = "PHASE"
	RuntimeTypeActivity   = "ACTIVITY"
	RuntimeTypeHeartbeat  = "HEARTBEAT"
	RuntimeTypeFinalizing = "FINALIZING"
	RuntimeTypeVerifying  = "VERIFYING"
	RuntimeTypeTerminal   = "TERMINAL"

	RuntimeEventRunStarted       = "RUN_STARTED"
	RuntimeEventFingerprint      = "FINGERPRINT"
	RuntimeEventResolution       = "RESOLUTION"
	RuntimeEventCollection       = "COLLECTION"
	RuntimeEventProviderStarted  = "PROVIDER_STARTED"
	RuntimeEventProviderFinished = "PROVIDER_FINISHED"
	RuntimeEventArtifactSealed   = "ARTIFACT_SEALED"
	RuntimeEventPulse            = "PULSE"
	RuntimeEventStarted          = "STARTED"
	RuntimeEventRunReturned      = "RUN_RETURNED"
)

// Validate enforces the exact runtime type/event pair contract.
func (e RuntimeEvent) Validate() error {
	valid := false
	switch e.Type {
	case RuntimeTypeStart:
		valid = e.Event == RuntimeEventRunStarted
	case RuntimeTypePhase:
		valid = e.Event == RuntimeEventFingerprint || e.Event == RuntimeEventResolution || e.Event == RuntimeEventCollection
	case RuntimeTypeActivity:
		valid = e.Event == RuntimeEventProviderStarted || e.Event == RuntimeEventProviderFinished || e.Event == RuntimeEventArtifactSealed
	case RuntimeTypeHeartbeat:
		valid = e.Event == RuntimeEventPulse
	case RuntimeTypeFinalizing, RuntimeTypeVerifying:
		valid = e.Event == RuntimeEventStarted
	case RuntimeTypeTerminal:
		valid = e.Event == RuntimeEventRunReturned
	}
	if !valid {
		return fmt.Errorf("invalid runtime event pair %q/%q", e.Type, e.Event)
	}
	return nil
}
