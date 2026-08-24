//go:build windows

package provider

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/execution"
)

const (
	evtOpenChannelPath = uint32(0x1)
)

type windowsEventLogAPI struct {
	openProc   *syscall.LazyProc
	exportProc *syscall.LazyProc
	closeProc  *syscall.LazyProc
	reason     execution.Reason
	err        error
}

func newPlatformEventLogAPI() eventLogAPI {
	return newWindowsEventLogAPI()
}

func newWindowsEventLogAPI() *windowsEventLogAPI {
	dll := syscall.NewLazyDLL("wevtapi.dll")
	api := &windowsEventLogAPI{openProc: dll.NewProc("EvtOpenLog"), exportProc: dll.NewProc("EvtExportLog"), closeProc: dll.NewProc("EvtClose"), reason: execution.ReasonNone}
	for _, p := range []*syscall.LazyProc{api.openProc, api.exportProc, api.closeProc} {
		if err := p.Find(); err != nil {
			api.reason = execution.ReasonAPIUnavailable
			api.err = err
			break
		}
	}
	return api
}

func (api *windowsEventLogAPI) availability() (execution.Reason, error) { return api.reason, api.err }

func (api *windowsEventLogAPI) openLog(channel string) (uintptr, error) {
	path, err := syscall.UTF16PtrFromString(channel)
	if err != nil {
		return 0, &eventLogFailure{reason: execution.ReasonProviderError, err: err}
	}
	result, _, callErr := api.openProc.Call(0, uintptr(unsafe.Pointer(path)), uintptr(evtOpenChannelPath))
	if result == 0 {
		return 0, &eventLogFailure{reason: classifyWin32(callErr), err: boundedEventLogWin32Error("EvtOpenLog", callErr)}
	}
	return result, nil
}

func (api *windowsEventLogAPI) exportLog(sessionPath, channel, target string, flags uint32) error {
	channelPtr, err := syscall.UTF16PtrFromString(channel)
	if err != nil {
		return &eventLogFailure{reason: execution.ReasonProviderError, err: err}
	}
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return &eventLogFailure{reason: execution.ReasonProviderError, err: err}
	}
	result, _, callErr := api.exportProc.Call(0, uintptr(unsafe.Pointer(channelPtr)), 0, uintptr(unsafe.Pointer(targetPtr)), uintptr(flags))
	if result == 0 {
		return &eventLogFailure{reason: classifyWin32(callErr), err: boundedEventLogWin32Error("EvtExportLog", callErr)}
	}
	return nil
}

func (api *windowsEventLogAPI) close(handle uintptr) error {
	result, _, callErr := api.closeProc.Call(handle)
	if result == 0 {
		return &eventLogFailure{reason: classifyWin32(callErr), err: boundedEventLogWin32Error("EvtClose", callErr)}
	}
	return nil
}

func classifyWin32(err error) execution.Reason {
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ERROR_ACCESS_DENIED:
			return execution.ReasonPrivilegeRequired
		case syscall.ERROR_FILE_NOT_FOUND, syscall.ERROR_PATH_NOT_FOUND:
			return execution.ReasonTargetStateRestricted
		}
	}
	return execution.ReasonProviderError
}

func boundedEventLogWin32Error(operation string, err error) error {
	if errno, ok := err.(syscall.Errno); ok {
		return fmt.Errorf("%s failed with Win32 error %d", operation, uint32(errno))
	}
	return fmt.Errorf("%s failed", operation)
}
