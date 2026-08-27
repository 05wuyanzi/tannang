// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package application_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/05wuyanzi/tannang/internal/application"
	"github.com/05wuyanzi/tannang/internal/evidence"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
	"github.com/05wuyanzi/tannang/internal/receipt"
)

func TestWindowsSuccessfulPackageLeavesOnlyPublishedOutput(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "package")
	if _, err := application.Collect(context.Background(), "available-collected", output); err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if err := integrity.Verify(output); err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	assertNoTemporarySiblings(t, parent)
}

func TestWindowsFailureBeforePublicationLeavesNoFinalOutput(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "package")
	err := evidence.Create(output, receipt.Record{}, nil)
	if err == nil {
		t.Fatal("Create() unexpectedly accepted an invalid receipt")
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatalf("failed construction published final output: %v", statErr)
	}
	assertNoTemporarySiblings(t, parent)
}

func TestWindowsParentJunctionRejectedByWriter(t *testing.T) {
	base := t.TempDir()
	targetParent := filepath.Join(base, "target-parent")
	if err := os.Mkdir(targetParent, 0o755); err != nil {
		t.Fatal(err)
	}
	junctionParent := filepath.Join(base, "junction-parent")
	makeJunction(t, junctionParent, targetParent)

	_, err := application.Collect(context.Background(), "available-collected", filepath.Join(junctionParent, "package"))
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Collect() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
	if _, err := os.Lstat(filepath.Join(targetParent, "package")); !os.IsNotExist(err) {
		t.Fatalf("writer created content through junction: %v", err)
	}
}

func TestWindowsOutputRootJunctionRejectedByWriter(t *testing.T) {
	base := t.TempDir()
	targetRoot := filepath.Join(base, "target-root")
	if err := os.Mkdir(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	outputJunction := filepath.Join(base, "package")
	makeJunction(t, outputJunction, targetRoot)

	_, err := application.Collect(context.Background(), "available-collected", outputJunction)
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Collect() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
}

func TestWindowsPackageRootJunctionRejectedByVerifier(t *testing.T) {
	packageRoot := createAvailablePackage(t)
	junctionRoot := filepath.Join(t.TempDir(), "package-junction")
	makeJunction(t, junctionRoot, packageRoot)

	err := integrity.Verify(junctionRoot)
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Verify() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
}

func TestWindowsPackageInternalJunctionRejectedByVerifier(t *testing.T) {
	packageRoot := createAvailablePackage(t)
	rawPath := filepath.Join(packageRoot, "raw")
	if err := os.Remove(rawPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside-target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, rawPath, target)

	err := integrity.Verify(packageRoot)
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Verify() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
}

func TestWindowsChildWriteRejectsJunctionParent(t *testing.T) {
	base := t.TempDir()
	root, err := pathsafe.CreateTemporarySibling(filepath.Join(base, "package"))
	if err != nil {
		t.Fatalf("CreateTemporarySibling() error: %v", err)
	}
	if err := root.Mkdir("meta", 0o755); err != nil {
		t.Fatalf("Mkdir() error: %v", err)
	}
	meta := filepath.Join(root.Path(), "meta")
	if err := os.Remove(meta); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "outside-target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, meta, target)

	err = root.WriteFile("meta/package.json", []byte("{}\n"), 0o644)
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("WriteFile() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
	if _, err := os.Lstat(filepath.Join(target, "package.json")); !os.IsNotExist(err) {
		t.Fatalf("child write crossed junction: %v", err)
	}
}

func TestWindowsTemporaryRootJunctionRejectedBeforePublish(t *testing.T) {
	base := t.TempDir()
	output := filepath.Join(base, "package")
	root, err := pathsafe.CreateTemporarySibling(output)
	if err != nil {
		t.Fatalf("CreateTemporarySibling() error: %v", err)
	}
	temporary := root.Path()
	if err := os.Remove(temporary); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "outside-target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, temporary, target)

	err = root.Publish()
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Publish() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe temporary root was published: %v", statErr)
	}
	if cleanupErr := root.Cleanup(); !pathsafe.HasCode(cleanupErr, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Cleanup() error = %v, want %s", cleanupErr, pathsafe.CodeReparsePointDetected)
	}
	if data, readErr := os.ReadFile(sentinel); readErr != nil || string(data) != "outside\n" {
		t.Fatalf("cleanup changed redirected target: %q, %v", data, readErr)
	}
}

func TestWindowsDirectorySymlinkRejectedByWriter(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "symlink-target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "symlink-parent")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Windows symbolic-link fixture unavailable without changing privileges or Developer Mode: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	_, err := application.Collect(context.Background(), "available-collected", filepath.Join(link, "package"))
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Collect() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
}

func TestWindowsFileSymlinkRejectedByVerifier(t *testing.T) {
	packageRoot := createAvailablePackage(t)
	report := filepath.Join(packageRoot, "reports", "summary.txt")
	if err := os.Remove(report); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside-report.txt")
	if err := os.WriteFile(target, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, report); err != nil {
		t.Skipf("Windows symbolic-link fixture unavailable without changing privileges or Developer Mode: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(report) })

	err := integrity.Verify(packageRoot)
	if !pathsafe.HasCode(err, pathsafe.CodeReparsePointDetected) {
		t.Fatalf("Verify() error = %v, want %s", err, pathsafe.CodeReparsePointDetected)
	}
}

func makeJunction(t *testing.T, junction, target string) {
	t.Helper()
	command := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target)
	output, err := command.CombinedOutput()
	t.Logf("junction fixture command: cmd.exe /d /c mklink /J %q %q", junction, target)
	t.Logf("junction fixture exit success=%t output=%s", err == nil, strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("create real Windows junction fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(junction); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove junction fixture: %v", err)
		}
	})
}

func assertNoTemporarySiblings(t *testing.T, parent string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(parent, ".tannang-package-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary package siblings remain: %v", matches)
	}
}
