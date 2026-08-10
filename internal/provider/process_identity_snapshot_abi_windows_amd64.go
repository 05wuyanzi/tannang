// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows && amd64

package provider

import "unsafe"

var _ [568]byte = [unsafe.Sizeof(processEntry32W{})]byte{}
var _ [16]byte = [unsafe.Offsetof(processEntry32W{}.DefaultHeapID)]byte{}
var _ [24]byte = [unsafe.Offsetof(processEntry32W{}.ModuleID)]byte{}
var _ [32]byte = [unsafe.Offsetof(processEntry32W{}.ParentProcessID)]byte{}
var _ [44]byte = [unsafe.Offsetof(processEntry32W{}.ExeFile)]byte{}
