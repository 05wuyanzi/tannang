// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If applicable, see <http://mozilla.org/MPL/2.0/>.

//go:build windows

package provider

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/execution"
)

const (
	computerNamePhysicalDNSHostname = 5
	processorArchitectureIntel      = 0
	processorArchitectureAMD64      = 9
)

type hostOSIdentityWindowsAPI struct {
	computerNameProc *syscall.LazyProc
	nativeInfoProc   *syscall.LazyProc
	rtlVersionProc   *syscall.LazyProc
	reason           execution.Reason
	err              error
}

func newPlatformHostOSIdentityAPI() hostOSIdentityAPI { return newHostOSIdentityWindowsAPI() }

func newHostOSIdentityWindowsAPI() *hostOSIdentityWindowsAPI {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	ntdll := syscall.NewLazyDLL("ntdll.dll")
	api := &hostOSIdentityWindowsAPI{computerNameProc: kernel32.NewProc("GetComputerNameExW"), nativeInfoProc: kernel32.NewProc("GetNativeSystemInfo"), rtlVersionProc: ntdll.NewProc("RtlGetVersion"), reason: execution.ReasonNone}
	for _, proc := range []*syscall.LazyProc{api.computerNameProc, api.nativeInfoProc, api.rtlVersionProc} {
		if err := proc.Find(); err != nil {
			api.reason, api.err = execution.ReasonAPIUnavailable, err
			break
		}
	}
	return api
}

func (a *hostOSIdentityWindowsAPI) availability() (execution.Reason, error) { return a.reason, a.err }

func (a *hostOSIdentityWindowsAPI) computerNameEx(buffer []uint16, size *uint32) (bool, error) {
	if a == nil || a.computerNameProc == nil || size == nil {
		return false, errors.New("GetComputerNameExW unavailable")
	}
	var pointer uintptr
	if len(buffer) > 0 {
		pointer = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result, _, callErr := a.computerNameProc.Call(uintptr(computerNamePhysicalDNSHostname), pointer, uintptr(unsafe.Pointer(size)))
	return result != 0, boundedHostOSIdentityCallError(callErr, result != 0)
}

type hostOSIdentityVersionInfo struct {
	Size             uint32
	MajorVersion     uint32
	MinorVersion     uint32
	BuildNumber      uint32
	PlatformID       uint32
	CSDVersion       [128]uint16
	ServicePackMajor uint16
	ServicePackMinor uint16
	SuiteMask        uint16
	ProductType      byte
	Reserved         byte
}

func (a *hostOSIdentityWindowsAPI) osVersion() (uint32, uint32, uint32, error) {
	if a == nil || a.rtlVersionProc == nil {
		return 0, 0, 0, errors.New("RtlGetVersion unavailable")
	}
	info := hostOSIdentityVersionInfo{Size: uint32(unsafe.Sizeof(hostOSIdentityVersionInfo{}))}
	status, _, _ := a.rtlVersionProc.Call(uintptr(unsafe.Pointer(&info)))
	if status != 0 || info.MajorVersion == 0 {
		return 0, 0, 0, fmt.Errorf("RtlGetVersion returned status %d", status)
	}
	return info.MajorVersion, info.MinorVersion, info.BuildNumber, nil
}

type hostOSIdentitySystemInfo struct {
	ProcessorArchitecture     uint16
	Reserved                  uint16
	PageSize                  uint32
	MinimumApplicationAddress uintptr
	MaximumApplicationAddress uintptr
	ActiveProcessorMask       uintptr
	NumberOfProcessors        uint32
	ProcessorType             uint32
	AllocationGranularity     uint32
	ProcessorLevel            uint16
	ProcessorRevision         uint16
}

func (a *hostOSIdentityWindowsAPI) nativeArchitecture() (string, error) {
	if a == nil || a.nativeInfoProc == nil {
		return "", errors.New("GetNativeSystemInfo unavailable")
	}
	var info hostOSIdentitySystemInfo
	a.nativeInfoProc.Call(uintptr(unsafe.Pointer(&info)))
	switch info.ProcessorArchitecture {
	case processorArchitectureIntel:
		return "x86", nil
	case processorArchitectureAMD64:
		return "amd64", nil
	default:
		return "", fmt.Errorf("unsupported processor architecture %d", info.ProcessorArchitecture)
	}
}

func boundedHostOSIdentityError(operation string, err error) error {
	if errno, ok := err.(syscall.Errno); ok {
		return fmt.Errorf("%s failed with Win32 error %d", operation, uint32(errno))
	}
	return fmt.Errorf("%s failed", operation)
}

func boundedHostOSIdentityCallError(err error, success bool) error {
	if success {
		return nil
	}
	return boundedHostOSIdentityError("GetComputerNameExW", err)
}
