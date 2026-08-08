// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/05wuyanzi/tannang/internal/cli"
)

func TestWindowsPathSafetyHasIndependentCLIExit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(
		context.Background(),
		[]string{"collect", "--synthetic", "available-collected", "--output", `relative\package`},
		&stdout,
		&stderr,
	)
	if code != cli.ExitPathSafety {
		t.Fatalf("collect exit = %d, want %d; stderr=%s", code, cli.ExitPathSafety, stderr.String())
	}
	if !strings.Contains(stderr.String(), "AMBIGUOUS_WINDOWS_PATH") {
		t.Fatalf("collect error lacks stable path-safety code: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"verify", `\\server\share\package`}, &stdout, &stderr)
	if code != cli.ExitPathSafety {
		t.Fatalf("verify exit = %d, want %d; stderr=%s", code, cli.ExitPathSafety, stderr.String())
	}
	if !strings.Contains(stderr.String(), "NETWORK_PATH_UNSUPPORTED") {
		t.Fatalf("verify error lacks stable path-safety code: %s", stderr.String())
	}
}

func TestWindowsCollectReservedFinalComponentStopsBeforeIO(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "CONOUT$")
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"collect", "--synthetic", "available-collected", "--output", output}, &stdout, &stderr)
	if code != cli.ExitPathSafety {
		t.Fatalf("collect exit = %d, want %d; stderr=%s", code, cli.ExitPathSafety, stderr.String())
	}
	if !strings.Contains(stderr.String(), "AMBIGUOUS_WINDOWS_PATH") {
		t.Fatalf("collect error lacks stable path-safety code: %s", stderr.String())
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("reserved output was created: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(parent, ".tannang-package-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary package was created: %v", matches)
	}
}
