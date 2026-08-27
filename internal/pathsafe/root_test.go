// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package pathsafe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputRootCreateFileAndRemoveFile(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	root, err := CreateTemporarySibling(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Cleanup() })
	if err := root.Mkdir("derived", 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := root.CreateFile("derived/artifact.ndjson", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatalf("created file state info=%+v err=%v", info, err)
	}
	if _, err := file.Write([]byte("candidate")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.CreateFile("derived/artifact.ndjson", 0o600); err == nil {
		t.Fatal("exclusive CreateFile unexpectedly overwrote an existing target")
	}
	if err := root.RemoveFile("derived/artifact.ndjson"); err != nil {
		t.Fatal(err)
	}
	if err := root.RemoveFile("derived/artifact.ndjson"); err != nil {
		t.Fatalf("safely absent RemoveFile returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.Path(), "derived", "artifact.ndjson")); !os.IsNotExist(err) {
		t.Fatalf("artifact still exists: %v", err)
	}
}

func TestOutputRootCreateAndRemoveFailClosed(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	root, err := CreateTemporarySibling(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Cleanup() })
	if err := root.Mkdir("derived", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := root.CreateFile("derived/wide.ndjson", 0o644); err == nil {
		t.Fatal("CreateFile accepted a widened mode")
	}
	for _, path := range []string{"../escape", "/absolute", "derived\\artifact.ndjson"} {
		if _, err := root.CreateFile(path, 0o600); err == nil {
			t.Fatalf("unsafe CreateFile path %q accepted", path)
		}
		if err := root.RemoveFile(path); err == nil {
			t.Fatalf("unsafe RemoveFile path %q accepted", path)
		}
	}
	if err := root.RemoveFile("derived"); err == nil {
		t.Fatal("RemoveFile accepted a directory")
	}
}

func TestOutputRootRemoveFileRejectsSymlink(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	root, err := CreateTemporarySibling(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Cleanup() })
	if err := root.Mkdir("derived", 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root.Path(), "derived", "redirect")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), target); err != nil {
		t.Skipf("symlink oracle unavailable: %v", err)
	}
	if err := root.RemoveFile("derived/redirect"); err == nil {
		t.Fatal("RemoveFile accepted a linked target")
	}
}
