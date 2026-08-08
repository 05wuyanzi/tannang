// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWindowsRenameRetryStateMachine(t *testing.T) {
	tests := []struct {
		name       string
		sequence   []error
		wantCalls  int
		wantDelays []time.Duration
	}{
		{name: "immediate success", sequence: []error{nil}, wantCalls: 1},
		{name: "access denied", sequence: []error{syscall.Errno(5), nil}, wantCalls: 2, wantDelays: []time.Duration{10 * time.Millisecond}},
		{name: "sharing violation", sequence: []error{syscall.Errno(32), nil}, wantCalls: 2, wantDelays: []time.Duration{10 * time.Millisecond}},
		{name: "lock violation", sequence: []error{syscall.Errno(33), nil}, wantCalls: 2, wantDelays: []time.Duration{10 * time.Millisecond}},
		{name: "retry exhaustion", sequence: []error{syscall.Errno(5), syscall.Errno(5), syscall.Errno(5), syscall.Errno(5), syscall.Errno(5)}, wantCalls: 5, wantDelays: []time.Duration{10 * time.Millisecond, 25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond}},
		{name: "unknown error", sequence: []error{syscall.Errno(1234)}, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var delays []time.Duration
			err := renameWithRetry("source", "destination", func(string, string) error {
				current := test.sequence[calls]
				calls++
				return current
			}, func() error { return nil }, func(delay time.Duration) { delays = append(delays, delay) })
			if calls != test.wantCalls {
				t.Fatalf("rename calls = %d, want %d", calls, test.wantCalls)
			}
			if len(delays) != len(test.wantDelays) {
				t.Fatalf("delays = %v, want %v", delays, test.wantDelays)
			}
			for i := range delays {
				if delays[i] != test.wantDelays[i] {
					t.Fatalf("delays = %v, want %v", delays, test.wantDelays)
				}
			}
			if test.name == "retry exhaustion" {
				var errno syscall.Errno
				if !errors.As(err, &errno) || errno != syscall.Errno(5) {
					t.Fatalf("final error = %v, errno = %v, want errno 5", err, errno)
				}
			}
		})
	}
}

func TestWindowsRenameRetryStopsWhenDestinationAppears(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "destination")
	sentinel := filepath.Join(destination, "sentinel.txt")
	calls := 0
	err := renameWithRetry(filepath.Join(parent, "source"), destination, func(string, string) error {
		calls++
		if err := os.Mkdir(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("existing\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return syscall.Errno(5)
	}, func() error {
		return ValidateOutputPath(destination)
	}, func(time.Duration) {})
	if calls != 1 {
		t.Fatalf("rename calls = %d, want 1", calls)
	}
	if !HasCode(err, CodeOutputAlreadyExists) {
		t.Fatalf("error = %v, want %s", err, CodeOutputAlreadyExists)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "existing\n" {
		t.Fatalf("existing destination was changed: %q, %v", data, err)
	}
}

func TestWindowsRenameRetryStopsWhenSourceBecomesUnsafe(t *testing.T) {
	calls := 0
	err := renameWithRetry("source", "destination", func(string, string) error {
		calls++
		return syscall.Errno(5)
	}, func() error {
		return reject(CodeReparsePointDetected, "scan package tree", "symbolic link or reparse point detected")
	}, func(time.Duration) {})
	if calls != 1 {
		t.Fatalf("rename calls = %d, want 1", calls)
	}
	if !HasCode(err, CodeReparsePointDetected) {
		t.Fatalf("error = %v, want %s", err, CodeReparsePointDetected)
	}
}

func TestWindowsSafeLocalTemporaryRoot(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	if err := ValidateOutputPath(output); err != nil {
		t.Fatalf("ValidateOutputPath() error: %v", err)
	}
	root, err := CreateTemporarySibling(output)
	if err != nil {
		t.Fatalf("CreateTemporarySibling() error: %v", err)
	}
	t.Cleanup(func() { _ = root.Cleanup() })
	if err := root.Mkdir("meta", 0o755); err != nil {
		t.Fatalf("Mkdir() error: %v", err)
	}
	if err := root.WriteFile("meta/test.txt", []byte("synthetic\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := root.Publish(); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}
}

func TestWindowsExistingOutputRejected(t *testing.T) {
	output := filepath.Join(t.TempDir(), "package")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	err := ValidateOutputPath(output)
	if !HasCode(err, CodeOutputAlreadyExists) {
		t.Fatalf("ValidateOutputPath() error = %v, want %s", err, CodeOutputAlreadyExists)
	}
}

func TestWindowsTemporarySiblingPublishesOnlyAtEnd(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "package")
	root, err := CreateTemporarySibling(output)
	if err != nil {
		t.Fatalf("CreateTemporarySibling() error: %v", err)
	}
	t.Cleanup(func() { _ = root.Cleanup() })
	temporary := root.Path()
	if filepath.Dir(temporary) != parent || temporary == output {
		t.Fatalf("temporary root %q is not a sibling of %q", temporary, output)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("final output exists before publication: %v", err)
	}
	if err := root.Mkdir("meta", 0o755); err != nil {
		t.Fatalf("Mkdir() error: %v", err)
	}
	if err := root.WriteFile("meta/test.txt", []byte("synthetic\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := root.Publish(); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}
	if root.Path() != output {
		t.Fatalf("published root path = %q, want %q", root.Path(), output)
	}
	if _, err := os.Lstat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary root remains after publication: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(output, "meta", "test.txt")); err != nil || string(data) != "synthetic\n" {
		t.Fatalf("published file = %q, %v", data, err)
	}
}

func TestWindowsPublishRefusesLateExistingDestination(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "package")
	root, err := CreateTemporarySibling(output)
	if err != nil {
		t.Fatalf("CreateTemporarySibling() error: %v", err)
	}
	t.Cleanup(func() { _ = root.Cleanup() })
	if err := root.WriteFile("payload.txt", []byte("temporary\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(output, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := root.Publish(); !HasCode(err, CodeOutputAlreadyExists) {
		t.Fatalf("Publish() error = %v, want %s", err, CodeOutputAlreadyExists)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "existing\n" {
		t.Fatalf("existing destination was changed: %q, %v", data, err)
	}
}

func TestWindowsUnsafePathClassesRejected(t *testing.T) {
	volume := filepath.VolumeName(t.TempDir())
	tests := []struct {
		name string
		path string
		code Code
	}{
		{"relative", `relative\package`, CodeAmbiguousWindowsPath},
		{"drive-relative", volume + `package`, CodeAmbiguousWindowsPath},
		{"volume-less-rooted", `\package`, CodeAmbiguousWindowsPath},
		{"traversal", volume + `\safe\..\package`, CodeAmbiguousWindowsPath},
		{"trailing-period", volume + `\safe\package.`, CodeAmbiguousWindowsPath},
		{"trailing-space", volume + `\safe\package `, CodeAmbiguousWindowsPath},
		{"alternate-stream", volume + `\safe\package:stream`, CodeAmbiguousWindowsPath},
		{"reserved-device", volume + `\safe\NUL.txt`, CodeAmbiguousWindowsPath},
		{"conin-dollar", volume + `\safe\CONIN$`, CodeAmbiguousWindowsPath},
		{"conin-dollar-extension", volume + `\safe\CONIN$.txt`, CodeAmbiguousWindowsPath},
		{"conout-dollar", volume + `\safe\CONOUT$`, CodeAmbiguousWindowsPath},
		{"conout-dollar-extension", volume + `\safe\CONOUT$.txt`, CodeAmbiguousWindowsPath},
		{"com-zero", volume + `\safe\COM0`, CodeAmbiguousWindowsPath},
		{"lpt-zero", volume + `\safe\LPT0`, CodeAmbiguousWindowsPath},
		{"wildcard", volume + `\safe\pack*age`, CodeAmbiguousWindowsPath},
		{"empty-component", volume + `\safe\\package`, CodeAmbiguousWindowsPath},
		{"short-name-alias", volume + `\PROGRA~1\package`, CodeAmbiguousWindowsPath},
		{"unc", `\\server\share\package`, CodeNetworkPathUnsupported},
		{"extended-namespace", `\\?\C:\safe\package`, CodeDeviceNamespaceUnsupported},
		{"device-namespace", `\\.\C:\safe\package`, CodeDeviceNamespaceUnsupported},
		{"nt-namespace", `\??\C:\safe\package`, CodeDeviceNamespaceUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := platformValidateAbsoluteLocalPath(test.path)
			if !HasCode(err, test.code) {
				t.Fatalf("path %q error = %v, want %s", test.path, err, test.code)
			}
		})
	}
}

func TestWindowsUnsafeChildPathsRejected(t *testing.T) {
	for _, path := range []string{
		"../escape",
		"meta/../escape",
		"meta\\file.json",
		"meta//file.json",
		"/absolute/file.json",
		"C:/volume/file.json",
		"meta/NUL.txt",
		"meta/CONOUT$",
		"reports/CONIN$.txt",
		"meta/file.json.",
	} {
		if err := ValidateRelativePath(path); err == nil {
			t.Errorf("ValidateRelativePath(%q) unexpectedly succeeded", path)
		}
	}
}

func TestWindowsDriveTypePolicy(t *testing.T) {
	for _, allowed := range []uint32{driveRemovable, driveFixed} {
		if err := validateDriveType(allowed); err != nil {
			t.Errorf("validateDriveType(%d) error: %v", allowed, err)
		}
	}
	if err := validateDriveType(driveRemote); !HasCode(err, CodeNetworkPathUnsupported) {
		t.Fatalf("remote drive error = %v, want %s", err, CodeNetworkPathUnsupported)
	}
	for _, rejected := range []uint32{0, 1, 5, 6} {
		if err := validateDriveType(rejected); !HasCode(err, CodeUnsafePath) {
			t.Errorf("validateDriveType(%d) error = %v, want %s", rejected, err, CodeUnsafePath)
		}
	}
}
