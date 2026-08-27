// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/05wuyanzi/tannang/internal/buildinfo"
)

func TestVersionReturnsOneBoundedJSONDocumentWithoutCollection(t *testing.T) {
	previous := newRealFirstStage
	calls := 0
	newRealFirstStage = func() (realFirstStage, error) { calls++; return nil, errors.New("must not run") }
	defer func() { newRealFirstStage = previous }()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 || calls != 0 {
		t.Fatalf("code=%d stderr=%q calls=%d", code, stderr.String(), calls)
	}
	var got buildinfo.Info
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&got); err != nil || got.BaseVersion != buildinfo.BaseVersion || got.ProductVersion == "" || got.GoVersion == "" || got.GOOS == "" || got.GOARCH == "" {
		t.Fatalf("version=%+v err=%v", got, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("version output has trailing content: %v", err)
	}
}

func TestVersionFailuresAndHelp(t *testing.T) {
	var stderr bytes.Buffer
	writer := &failingTerminalWriter{limit: 0, err: errors.New("injected")}
	if code := Run(context.Background(), []string{"version"}, writer, &stderr); code != exitTerminalOutput || stderr.String() != "failed to write command result\n" {
		t.Fatalf("writer failure code=%d stderr=%q", code, stderr.String())
	}
	for _, args := range [][]string{{"version", "extra"}, {"unknown"}} {
		var stdout bytes.Buffer
		stderr.Reset()
		if code := Run(context.Background(), args, &stdout, &stderr); code != ExitUsage {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	var stdout bytes.Buffer
	stderr.Reset()
	if code := Run(context.Background(), []string{"help"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "tannang version") {
		t.Fatalf("help code=%d stdout=%q", code, stdout.String())
	}
}
