// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package provider

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestProcessEntry32WRuntimeLayoutMatchesCurrentWindowsArchitecture(t *testing.T) {
	t.Parallel()
	type layout struct {
		size            uintptr
		defaultHeapID   uintptr
		moduleID        uintptr
		parentProcessID uintptr
		exeFile         uintptr
	}
	want := layout{}
	switch unsafe.Sizeof(uintptr(0)) {
	case 8:
		want = layout{size: 568, defaultHeapID: 16, moduleID: 24, parentProcessID: 32, exeFile: 44}
	case 4:
		want = layout{size: 556, defaultHeapID: 12, moduleID: 16, parentProcessID: 24, exeFile: 36}
	default:
		t.Fatalf("unsupported uintptr size %d", unsafe.Sizeof(uintptr(0)))
	}
	got := layout{
		size:            unsafe.Sizeof(processEntry32W{}),
		defaultHeapID:   unsafe.Offsetof(processEntry32W{}.DefaultHeapID),
		moduleID:        unsafe.Offsetof(processEntry32W{}.ModuleID),
		parentProcessID: unsafe.Offsetof(processEntry32W{}.ParentProcessID),
		exeFile:         unsafe.Offsetof(processEntry32W{}.ExeFile),
	}
	if got != want {
		t.Fatalf("PROCESSENTRY32W layout = %+v, want %+v", got, want)
	}
}

func TestToolHelpPrimaryReturnValuesOverrideStaleLastError(t *testing.T) {
	t.Parallel()
	stale := syscall.Errno(5)
	handle, err := interpretSnapshotHandle(123, stale)
	if err != nil || handle != 123 {
		t.Fatalf("successful snapshot handle = %d, %v; want 123, nil", handle, err)
	}
	ok, err := interpretEnumerationResult("Process32FirstW", 1, stale)
	if err != nil || !ok {
		t.Fatalf("successful enumeration = %v, %v; want true, nil", ok, err)
	}
	if err := interpretCloseResult(1, stale); err != nil {
		t.Fatalf("successful CloseHandle rejected stale last-error: %v", err)
	}
}

func TestToolHelpFailureAndNormalExhaustionSemantics(t *testing.T) {
	t.Parallel()
	if handle, err := interpretSnapshotHandle(invalidProcessSnapshotHandle, syscall.Errno(5)); err == nil || handle != 0 || !strings.Contains(err.Error(), "5") {
		t.Fatalf("invalid snapshot handle = %d, %v; want bounded Win32 error 5", handle, err)
	}
	ok, err := interpretEnumerationResult("Process32NextW", 0, syscall.ERROR_NO_MORE_FILES)
	if err != nil || ok {
		t.Fatalf("normal enumeration exhaustion = %v, %v; want false, nil", ok, err)
	}
	ok, err = interpretEnumerationResult("Process32NextW", 0, syscall.Errno(5))
	if err == nil || ok || !strings.Contains(err.Error(), "5") {
		t.Fatalf("abnormal enumeration failure = %v, %v; want false and bounded Win32 error 5", ok, err)
	}
	if err := interpretCloseResult(0, syscall.Errno(6)); err == nil || !strings.Contains(err.Error(), "6") {
		t.Fatalf("failed CloseHandle error = %v; want bounded Win32 error 6", err)
	}
}

func TestToolHelpProcessOnlyConstants(t *testing.T) {
	t.Parallel()
	if th32csSnapProcess != 0x00000002 {
		t.Fatalf("TH32CS_SNAPPROCESS = %#x, want 0x2", th32csSnapProcess)
	}
	if invalidProcessSnapshotHandle != ^uintptr(0) {
		t.Fatalf("INVALID_HANDLE_VALUE = %#x, want all-bits-one uintptr", invalidProcessSnapshotHandle)
	}
}
