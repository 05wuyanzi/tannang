// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/05wuyanzi/tannang/internal/pathsafe"
)

func TestHashFileUsesGuardedPathAndExactBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "derived"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("first-stage\nartifact\n")
	if err := pathsafe.WriteNewFile(root, "derived/artifact.ndjson", data, 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := HashFile(root, "derived/artifact.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if entry.Path != "derived/artifact.ndjson" || entry.Size != int64(len(data)) || entry.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("HashFile entry = %+v", entry)
	}
	for _, unsafe := range []string{"../escape", "derived\\artifact.ndjson", "/absolute"} {
		if _, err := HashFile(root, unsafe); err == nil {
			t.Fatalf("unsafe HashFile path %q unexpectedly accepted", unsafe)
		}
	}
}

func TestManifestVerifyRejectsPostGenerateMutation(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"derived", "hashes"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "derived", "artifact.ndjson")
	if err := pathsafe.WriteNewFile(root, "derived/artifact.ndjson", []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Generate(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(root); err == nil {
		t.Fatal("manifest verification accepted post-generate artifact mutation")
	}
}
