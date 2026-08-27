// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package provider

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/execution"
)

const th32csSnapProcess = 0x00000002

var invalidProcessSnapshotHandle = ^uintptr(0)

type windowsProcessSnapshotAPI struct {
	createProc          *syscall.LazyProc
	firstProc           *syscall.LazyProc
	nextProc            *syscall.LazyProc
	closeProc           *syscall.LazyProc
	availabilityReason  execution.Reason
	availabilityFailure error
}

func newPlatformProcessSnapshotAPI() processSnapshotAPI {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	api := &windowsProcessSnapshotAPI{
		createProc:         kernel32.NewProc("CreateToolhelp32Snapshot"),
		firstProc:          kernel32.NewProc("Process32FirstW"),
		nextProc:           kernel32.NewProc("Process32NextW"),
		closeProc:          kernel32.NewProc("CloseHandle"),
		availabilityReason: execution.ReasonNone,
	}
	procedures := []struct {
		name string
		proc *syscall.LazyProc
	}{
		{name: "CreateToolhelp32Snapshot", proc: api.createProc},
		{name: "Process32FirstW", proc: api.firstProc},
		{name: "Process32NextW", proc: api.nextProc},
		{name: "CloseHandle", proc: api.closeProc},
	}
	for _, procedure := range procedures {
		if err := procedure.proc.Find(); err != nil {
			api.availabilityReason = execution.ReasonAPIUnavailable
			api.availabilityFailure = fmt.Errorf("resolve %s: %w", procedure.name, err)
			break
		}
	}
	return api
}

func (api *windowsProcessSnapshotAPI) availability() (execution.Reason, error) {
	return api.availabilityReason, api.availabilityFailure
}

func (api *windowsProcessSnapshotAPI) create() (uintptr, error) {
	result, _, callErr := api.createProc.Call(th32csSnapProcess, 0)
	return interpretSnapshotHandle(result, callErr)
}

func (api *windowsProcessSnapshotAPI) first(handle uintptr, entry *processEntry32W) (bool, error) {
	entry.Size = uint32(unsafe.Sizeof(*entry))
	result, _, callErr := api.firstProc.Call(handle, uintptr(unsafe.Pointer(entry)))
	return interpretEnumerationResult("Process32FirstW", result, callErr)
}

func (api *windowsProcessSnapshotAPI) next(handle uintptr, entry *processEntry32W) (bool, error) {
	entry.Size = uint32(unsafe.Sizeof(*entry))
	result, _, callErr := api.nextProc.Call(handle, uintptr(unsafe.Pointer(entry)))
	return interpretEnumerationResult("Process32NextW", result, callErr)
}

func (api *windowsProcessSnapshotAPI) close(handle uintptr) error {
	result, _, callErr := api.closeProc.Call(handle)
	return interpretCloseResult(result, callErr)
}

func interpretSnapshotHandle(result uintptr, callErr error) (uintptr, error) {
	if result != invalidProcessSnapshotHandle {
		return result, nil
	}
	return 0, boundedWindowsProcessError("CreateToolhelp32Snapshot", callErr)
}

func interpretEnumerationResult(operation string, result uintptr, callErr error) (bool, error) {
	if result != 0 {
		return true, nil
	}
	if errors.Is(callErr, syscall.ERROR_NO_MORE_FILES) {
		return false, nil
	}
	return false, boundedWindowsProcessError(operation, callErr)
}

func interpretCloseResult(result uintptr, callErr error) error {
	if result != 0 {
		return nil
	}
	return boundedWindowsProcessError("CloseHandle", callErr)
}

func boundedWindowsProcessError(operation string, callErr error) error {
	if errno, ok := callErr.(syscall.Errno); ok {
		return fmt.Errorf("%s failed with Win32 error %d", operation, uint32(errno))
	}
	if callErr == nil {
		return fmt.Errorf("%s failed without a Win32 error", operation)
	}
	return fmt.Errorf("%s failed", operation)
}
