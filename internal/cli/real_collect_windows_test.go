// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsRealCollectPathSafetyStopsBeforeFactory(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "package")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		output string
	}{
		{"UNC", `\\server\share\package`},
		{"device namespace", `\\?\C:\package`},
		{"existing output", existing},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			restore := replaceRealFactory(func() (realFirstStage, error) {
				calls++
				return &fakeRealFirstStage{}, nil
			})
			defer restore()
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"collect", "--output", test.output}, &stdout, &stderr)
			if code != ExitPathSafety || calls != 0 {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
			}
		})
	}
}
