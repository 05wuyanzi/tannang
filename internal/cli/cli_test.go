// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/05wuyanzi/tannang/internal/cli"
)

func TestCollectExitStatesAndVerify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		code    int
	}{
		{"available-collected", cli.ExitOK},
		{"degraded-partial", cli.ExitPartial},
		{"unavailable", cli.ExitSkipped},
		{"privilege-blocked", cli.ExitBlocked},
		{"provider-failure", cli.ExitProviderError},
	}
	for _, test := range tests {
		test := test
		t.Run(test.fixture, func(t *testing.T) {
			t.Parallel()
			output := filepath.Join(t.TempDir(), "package")
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{"collect", "--synthetic", test.fixture, "--output", output}, &stdout, &stderr)
			if code != test.code {
				t.Fatalf("collect exit = %d, want %d; stdout=%s stderr=%s", code, test.code, stdout.String(), stderr.String())
			}
			stdout.Reset()
			stderr.Reset()
			if code := cli.Run(context.Background(), []string{"verify", output}, &stdout, &stderr); code != cli.ExitOK {
				t.Fatalf("verify exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}
}
