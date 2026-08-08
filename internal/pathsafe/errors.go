// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package pathsafe enforces fail-closed package path and filesystem rules.
package pathsafe

import (
	"errors"
	"fmt"
)

// Code identifies a path-safety failure without mixing it into provider state.
type Code string

const (
	CodeUnsafePath                 Code = "UNSAFE_PATH"
	CodeReparsePointDetected       Code = "REPARSE_POINT_DETECTED"
	CodeNetworkPathUnsupported     Code = "NETWORK_PATH_UNSUPPORTED"
	CodeDeviceNamespaceUnsupported Code = "DEVICE_NAMESPACE_UNSUPPORTED"
	CodeAmbiguousWindowsPath       Code = "AMBIGUOUS_WINDOWS_PATH"
	CodeOutputAlreadyExists        Code = "OUTPUT_ALREADY_EXISTS"
	CodePathEscapesRoot            Code = "PATH_ESCAPES_ROOT"
	CodeUnsafePackageTree          Code = "UNSAFE_PACKAGE_TREE"
)

// Error is a stable, privacy-limited path-safety error.
type Error struct {
	Code      Code
	Operation string
	Detail    string
	Err       error
}

func (e *Error) Error() string {
	message := fmt.Sprintf("path safety %s", e.Code)
	if e.Operation != "" {
		message += " during " + e.Operation
	}
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	return message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func reject(code Code, operation, detail string) error {
	return &Error{Code: code, Operation: operation, Detail: detail}
}

func rejectCause(code Code, operation, detail string, err error) error {
	return &Error{Code: code, Operation: operation, Detail: detail, Err: err}
}

// IsSafetyError reports whether err contains a path-safety rejection.
func IsSafetyError(err error) bool {
	var target *Error
	return errors.As(err, &target)
}

// HasCode reports whether err contains a specific path-safety code.
func HasCode(err error, code Code) bool {
	if err == nil {
		return false
	}
	var target *Error
	if errors.As(err, &target) && target.Code == code {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if HasCode(child, code) {
				return true
			}
		}
		return false
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return HasCode(wrapped.Unwrap(), code)
	}
	return false
}
