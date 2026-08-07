// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package fingerprint

import (
	"errors"
	"strings"
)

// TargetFingerprint is supplied by a trusted fixture in the genesis build.
// It is never populated from the running host by this package.
type TargetFingerprint struct {
	Platform     string `json:"platform"`
	OSFamily     string `json:"os_family"`
	Version      string `json:"version"`
	Build        string `json:"build"`
	Architecture string `json:"architecture"`
	Privilege    string `json:"privilege"`
	Elevated     bool   `json:"elevated"`
	RuntimeLane  string `json:"runtime_lane"`
}

// Validate checks only contract completeness; it performs no host probes.
func (f TargetFingerprint) Validate() error {
	values := map[string]string{
		"platform":     f.Platform,
		"os_family":    f.OSFamily,
		"version":      f.Version,
		"build":        f.Build,
		"architecture": f.Architecture,
		"privilege":    f.Privilege,
		"runtime_lane": f.RuntimeLane,
	}
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
	}
	return nil
}
