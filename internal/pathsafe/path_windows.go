// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package pathsafe

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
)

var getDriveTypeW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW")

func platformValidateAbsoluteLocalPath(input string) (string, error) {
	if input == "" {
		return "", reject(CodeUnsafePath, "validate path", "path must be explicitly specified")
	}
	lower := strings.ToLower(input)
	devicePrefixes := []string{`\\?\`, `\\.\`, `\??\`, `\\??\`, `\device\`, `\globalroot\`, `//?/`, `//./`}
	for _, prefix := range devicePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return "", reject(CodeDeviceNamespaceUnsupported, "validate path", "Windows device and special namespaces are unsupported")
		}
	}
	if strings.HasPrefix(input, `\\`) || strings.HasPrefix(input, `//`) {
		return "", reject(CodeNetworkPathUnsupported, "validate path", "UNC and network paths are unsupported")
	}
	if strings.Contains(input, "/") {
		return "", reject(CodeAmbiguousWindowsPath, "validate path", "Windows paths must use backslash separators")
	}
	volume := filepath.VolumeName(input)
	if len(volume) != 2 || volume[1] != ':' || !isASCIILetter(volume[0]) {
		return "", reject(CodeAmbiguousWindowsPath, "validate path", "path must use an explicit drive-letter root")
	}
	root := volume + `\`
	if !strings.HasPrefix(strings.ToLower(input), strings.ToLower(root)) || !filepath.IsAbs(input) {
		return "", reject(CodeAmbiguousWindowsPath, "validate path", "relative and drive-relative paths are unsupported")
	}
	if filepath.Clean(input) != input {
		return "", reject(CodeAmbiguousWindowsPath, "validate path", "path must already be canonical")
	}
	remainder := strings.TrimPrefix(input, input[:len(root)])
	if remainder == "" {
		return "", reject(CodeAmbiguousWindowsPath, "validate path", "a volume root is not a package path")
	}
	for _, component := range strings.Split(remainder, `\`) {
		if err := validateWindowsComponent(component); err != nil {
			return "", err
		}
	}
	driveType, err := queryDriveType(root)
	if err != nil {
		return "", rejectCause(CodeUnsafePath, "classify storage", "drive type cannot be determined", err)
	}
	if err := validateDriveType(driveType); err != nil {
		return "", err
	}
	return input, nil
}

func platformValidateExistingPath(path string) error {
	abs, err := platformValidateAbsoluteLocalPath(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(abs)
	current := volume + `\`
	if err := rejectReparseAttribute(current); err != nil {
		return err
	}
	remainder := strings.TrimPrefix(abs, abs[:len(current)])
	for _, component := range strings.Split(remainder, `\`) {
		current = filepath.Join(current, component)
		if err := rejectReparseAttribute(current); err != nil {
			return err
		}
	}
	return nil
}

func platformValidateRelativeComponent(component string) error {
	return validateWindowsComponent(component)
}

func validateWindowsComponent(component string) error {
	if component == "" || component == "." || component == ".." {
		return reject(CodeAmbiguousWindowsPath, "validate path", "path contains an empty or relative component")
	}
	if strings.HasSuffix(component, " ") || strings.HasSuffix(component, ".") {
		return reject(CodeAmbiguousWindowsPath, "validate path", "path component has a trailing space or period")
	}
	for _, character := range component {
		if character < 32 || strings.ContainsRune(`<>:"/\|?*`, character) {
			return reject(CodeAmbiguousWindowsPath, "validate path", "path component contains a reserved character")
		}
	}
	base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
	if isReservedDOSName(base) {
		return reject(CodeAmbiguousWindowsPath, "validate path", "path component uses a reserved DOS device name")
	}
	if looksLikeShortNameAlias(base) {
		return reject(CodeAmbiguousWindowsPath, "validate path", "path component resembles an 8.3 alias")
	}
	return nil
}

func isReservedDOSName(base string) bool {
	switch base {
	case "CON", "CONIN$", "CONOUT$", "PRN", "AUX", "NUL", "CLOCK$":
		return true
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if strings.HasPrefix(base, prefix) {
			suffix := strings.TrimPrefix(base, prefix)
			if len(suffix) == 1 && suffix[0] >= '0' && suffix[0] <= '9' {
				return true
			}
			if suffix == "¹" || suffix == "²" || suffix == "³" {
				return true
			}
		}
	}
	return false
}

func looksLikeShortNameAlias(component string) bool {
	tilde := strings.LastIndexByte(component, '~')
	if tilde < 1 || tilde > 6 || tilde == len(component)-1 {
		return false
	}
	for _, character := range component[tilde+1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validateDriveType(driveType uint32) error {
	switch driveType {
	case driveRemovable, driveFixed:
		return nil
	case driveRemote:
		return reject(CodeNetworkPathUnsupported, "classify storage", "mapped remote drives are unsupported")
	default:
		return reject(CodeUnsafePath, "classify storage", fmt.Sprintf("drive type %d is not allowed", driveType))
	}
}

func queryDriveType(root string) (uint32, error) {
	pointer, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	result, _, callErr := getDriveTypeW.Call(uintptr(unsafe.Pointer(pointer)))
	if result == 0 && callErr != syscall.Errno(0) {
		return 0, callErr
	}
	return uint32(result), nil
}

func rejectReparseAttribute(path string) error {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return rejectCause(CodeUnsafePath, "inspect filesystem path", "path cannot be represented for Windows", err)
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return rejectCause(CodeUnsafePath, "inspect filesystem path", "file attributes cannot be read", err)
	}
	if attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return reject(CodeReparsePointDetected, "inspect filesystem path", "a protected path component is a reparse point")
	}
	return nil
}

func platformRenameNoReplace(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return syscall.MoveFile(sourcePointer, destinationPointer)
}

func platformIsRetryableRenameError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case syscall.Errno(5), syscall.Errno(32), syscall.Errno(33):
		return true
	default:
		return false
	}
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
