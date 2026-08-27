// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package provider

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/execution"
)

type windowsTransportTableAPI struct {
	tcpProc *syscall.LazyProc
	udpProc *syscall.LazyProc
	reason  error
}

func newPlatformTransportTableAPI() transportTableAPI {
	dll := syscall.NewLazyDLL("iphlpapi.dll")
	api := &windowsTransportTableAPI{tcpProc: dll.NewProc("GetExtendedTcpTable"), udpProc: dll.NewProc("GetExtendedUdpTable")}
	for name, proc := range map[string]*syscall.LazyProc{"GetExtendedTcpTable": api.tcpProc, "GetExtendedUdpTable": api.udpProc} {
		if err := proc.Find(); err != nil {
			api.reason = fmt.Errorf("resolve %s: %w", name, err)
			break
		}
	}
	return api
}

func (a *windowsTransportTableAPI) availability() (execution.Reason, error) {
	if a == nil || a.reason != nil {
		return execution.ReasonAPIUnavailable, a.reason
	}
	return execution.ReasonNone, nil
}

func (a *windowsTransportTableAPI) getExtendedTcpTable(buffer []byte, size *uint32, order bool, family, table uint32) uint32 {
	if a == nil || a.tcpProc == nil || size == nil {
		return transportErrorNotSupported
	}
	var pointer uintptr
	if len(buffer) > 0 {
		pointer = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result, _, _ := a.tcpProc.Call(pointer, uintptr(unsafe.Pointer(size)), boolToUintptr(order), uintptr(family), uintptr(table), 0)
	return uint32(result)
}

func (a *windowsTransportTableAPI) getExtendedUdpTable(buffer []byte, size *uint32, order bool, family, table uint32) uint32 {
	if a == nil || a.udpProc == nil || size == nil {
		return transportErrorNotSupported
	}
	var pointer uintptr
	if len(buffer) > 0 {
		pointer = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result, _, _ := a.udpProc.Call(pointer, uintptr(unsafe.Pointer(size)), boolToUintptr(order), uintptr(family), uintptr(table), 0)
	return uint32(result)
}

func boolToUintptr(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}
