// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package buildinfo exposes bounded collector build provenance.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

const BaseVersion = "0.0.0-pre-alpha"

type Info struct {
	BaseVersion    string `json:"base_version"`
	ProductVersion string `json:"product_version"`
	VCS            string `json:"vcs"`
	SourceRevision string `json:"source_revision"`
	SourceModified bool   `json:"source_modified"`
	GoVersion      string `json:"go_version"`
	GOOS           string `json:"goos"`
	GOARCH         string `json:"goarch"`
}

// Current returns the build identity embedded by the Go toolchain.
func Current() Info {
	value, ok := debug.ReadBuildInfo()
	return fromBuildInfo(value, ok, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func fromBuildInfo(value *debug.BuildInfo, ok bool, fallbackGoVersion, goos, goarch string) Info {
	info := Info{
		BaseVersion:    BaseVersion,
		ProductVersion: BaseVersion + "+source.unknown",
		VCS:            "unknown",
		SourceRevision: "unknown",
		SourceModified: true,
		GoVersion:      fallbackGoVersion,
		GOOS:           goos,
		GOARCH:         goarch,
	}
	if !ok || value == nil {
		return info
	}
	if value.GoVersion != "" {
		info.GoVersion = value.GoVersion
	}
	settings := make(map[string]string, len(value.Settings))
	for _, setting := range value.Settings {
		settings[setting.Key] = setting.Value
	}
	if vcs := strings.TrimSpace(settings["vcs"]); vcs != "" {
		info.VCS = vcs
	}
	modified, modifiedValid := parseBuildBool(settings["vcs.modified"])
	info.SourceModified = modified || !modifiedValid
	revision := settings["vcs.revision"]
	if info.VCS != "git" || !validGitRevision(revision) {
		return info
	}
	info.SourceRevision = revision
	info.ProductVersion = BaseVersion + "+git." + revision
	if info.SourceModified {
		info.ProductVersion += ".modified"
	}
	return info
}

func parseBuildBool(value string) (bool, bool) {
	switch value {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return true, false
	}
}

func validGitRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
