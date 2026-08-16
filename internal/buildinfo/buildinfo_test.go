// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package buildinfo

import (
	"runtime/debug"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func TestBuildIdentityParsing(t *testing.T) {
	tests := map[string]struct {
		settings  []debug.BuildSetting
		ok        bool
		wantVCS   string
		wantRev   string
		modified  bool
		version   string
		goVersion string
	}{
		"clean git":        {settings: buildSettings("git", testRevision, "false"), ok: true, wantVCS: "git", wantRev: testRevision, version: BaseVersion + "+git." + testRevision, goVersion: "go-test"},
		"modified git":     {settings: buildSettings("git", testRevision, "true"), ok: true, wantVCS: "git", wantRev: testRevision, modified: true, version: BaseVersion + "+git." + testRevision + ".modified", goVersion: "go-test"},
		"unknown":          {ok: false, wantVCS: "unknown", wantRev: "unknown", modified: true, version: BaseVersion + "+source.unknown", goVersion: "fallback"},
		"unknown vcs":      {settings: buildSettings("", testRevision, "false"), ok: true, wantVCS: "unknown", wantRev: "unknown", version: BaseVersion + "+source.unknown", goVersion: "go-test"},
		"missing revision": {settings: buildSettings("git", "", "false"), ok: true, wantVCS: "git", wantRev: "unknown", version: BaseVersion + "+source.unknown", goVersion: "go-test"},
		"bad revision":     {settings: buildSettings("git", "ABCDEF", "false"), ok: true, wantVCS: "git", wantRev: "unknown", version: BaseVersion + "+source.unknown", goVersion: "go-test"},
		"bad modified":     {settings: buildSettings("git", testRevision, "False"), ok: true, wantVCS: "git", wantRev: testRevision, modified: true, version: BaseVersion + "+git." + testRevision + ".modified", goVersion: "go-test"},
		"non git":          {settings: buildSettings("hg", testRevision, "false"), ok: true, wantVCS: "hg", wantRev: "unknown", version: BaseVersion + "+source.unknown", goVersion: "go-test"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := fromBuildInfo(&debug.BuildInfo{GoVersion: "go-test", Settings: test.settings}, test.ok, "fallback", "windows", "amd64")
			if got.BaseVersion != BaseVersion || got.ProductVersion != test.version || got.VCS != test.wantVCS || got.SourceRevision != test.wantRev || got.SourceModified != test.modified || got.GoVersion != test.goVersion || got.GOOS != "windows" || got.GOARCH != "amd64" {
				t.Fatalf("identity = %+v", got)
			}
		})
	}
}

func buildSettings(vcs, revision, modified string) []debug.BuildSetting {
	settings := []debug.BuildSetting{{Key: "vcs", Value: vcs}, {Key: "vcs.modified", Value: modified}}
	if revision != "" {
		settings = append(settings, debug.BuildSetting{Key: "vcs.revision", Value: revision})
	}
	return settings
}
