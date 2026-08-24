// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package pathsafe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OutputRoot is a newly created package root guarded for child operations.
type OutputRoot struct {
	path        string
	destination string
	complete    bool
}

const integrationArtifactFileMode fs.FileMode = 0o600

// ValidateOutputPath validates a new output location without creating it.
func ValidateOutputPath(output string) error {
	abs, err := platformValidateAbsoluteLocalPath(output)
	if err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return reject(CodeAmbiguousWindowsPath, "validate output", "output must name a new child directory")
	}
	if err := validateExistingDirectory(parent, "validate output parent"); err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	switch {
	case err == nil:
		if err := platformValidateExistingPath(abs); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return reject(CodeReparsePointDetected, "validate output", "output root is a link or reparse point")
		}
		return reject(CodeOutputAlreadyExists, "validate output", "output package already exists; overwrite is refused")
	case !errors.Is(err, os.ErrNotExist):
		return rejectCause(CodeUnsafePath, "validate output", "output root state cannot be determined", err)
	default:
		return nil
	}
}

// CreateTemporarySibling creates a guarded construction root beside output.
func CreateTemporarySibling(output string) (*OutputRoot, error) {
	if err := ValidateOutputPath(output); err != nil {
		return nil, err
	}
	destination, err := platformValidateAbsoluteLocalPath(output)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(destination)
	if err := validateExistingDirectory(parent, "validate temporary package parent"); err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp(parent, ".tannang-package-")
	if err != nil {
		return nil, rejectCause(CodeUnsafePath, "create temporary package", "temporary sibling could not be created", err)
	}
	root := &OutputRoot{path: temporary, destination: destination}
	fail := func(cause error) (*OutputRoot, error) {
		return nil, errors.Join(cause, root.Cleanup())
	}
	relative, err := filepath.Rel(parent, temporary)
	if err != nil || relative == "." || filepath.IsAbs(relative) || filepath.Dir(relative) != "." {
		return fail(rejectCause(CodePathEscapesRoot, "create temporary package", "temporary package is not a direct sibling of the destination", err))
	}
	if err := validateExistingDirectory(parent, "revalidate temporary package parent"); err != nil {
		return fail(err)
	}
	if err := validateExistingDirectory(temporary, "revalidate temporary package root"); err != nil {
		return fail(err)
	}
	return root, nil
}

// Path returns the absolute protected root.
func (r *OutputRoot) Path() string {
	return r.path
}

// Mkdir creates one new package-relative directory.
func (r *OutputRoot) Mkdir(relative string, perm fs.FileMode) error {
	full, err := prepareNewChild(r.path, relative, "create package directory")
	if err != nil {
		return err
	}
	if err := os.Mkdir(full, perm); err != nil {
		return rejectCause(CodeUnsafePath, "create package directory", "child directory creation failed", err)
	}
	return validateExistingDirectory(full, "revalidate package directory")
}

// WriteFile creates one new package-relative file without overwrite semantics.
func (r *OutputRoot) WriteFile(relative string, data []byte, perm fs.FileMode) error {
	return WriteNewFile(r.path, relative, data, perm)
}

// CreateFile creates one fresh, exclusive, empty regular file for a streaming
// artifact. The bounded mode is intentionally fixed so callers cannot widen
// the package's file permissions through this integration seam.
func (r *OutputRoot) CreateFile(relative string, perm fs.FileMode) (*os.File, error) {
	if r == nil || r.complete {
		return nil, reject(CodeUnsafePath, "create package file", "output root is unavailable for staging")
	}
	if perm.Perm() != integrationArtifactFileMode {
		return nil, reject(CodeUnsafePath, "create package file", "streaming artifact mode is outside the fixed policy")
	}
	full, err := prepareNewChild(r.path, relative, "create package file")
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, integrationArtifactFileMode)
	if err != nil {
		return nil, rejectCause(CodeUnsafePath, "create package file", "exclusive file creation failed", err)
	}
	if err := validateExistingRegularFile(full, "revalidate created package file"); err != nil {
		_ = file.Close()
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, rejectCause(CodeUnsafePackageTree, "revalidate created package file", "created handle is not a regular file", statErr)
	}
	if info.Size() != 0 {
		_ = file.Close()
		return nil, reject(CodeUnsafePackageTree, "revalidate created package file", "created streaming file is not empty")
	}
	return file, nil
}

// ReserveFilePath validates a fresh package-relative child without creating
// it. It is used only by native Providers whose documented export API owns the
// file creation operation.
func (r *OutputRoot) ReserveFilePath(relative string) (string, error) {
	if r == nil || r.complete {
		return "", reject(CodeUnsafePath, "reserve package file", "output root is unavailable for staging")
	}
	return prepareNewChild(r.path, relative, "reserve package file")
}

// ValidateExistingFile revalidates a caller-owned export target as an ordinary
// non-reparse file beneath this protected root.
func (r *OutputRoot) ValidateExistingFile(relative string) error {
	if r == nil || r.complete {
		return reject(CodeUnsafePath, "validate package file", "output root is unavailable for staging")
	}
	full, err := prepareExistingChild(r.path, relative, "validate package file")
	if err != nil {
		return err
	}
	return validateExistingRegularFile(full, "validate package file")
}

// RemoveFile removes exactly one guarded package-relative regular file. A
// safely verified absent target is already in the desired terminal state.
func (r *OutputRoot) RemoveFile(relative string) error {
	if r == nil || r.complete {
		return reject(CodeUnsafePath, "remove package file", "output root is unavailable for mutation")
	}
	full, err := prepareExistingChild(r.path, relative, "remove package file")
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return rejectCause(CodeUnsafePath, "remove package file", "target state cannot be determined", err)
	}
	if err := platformValidateExistingPath(full); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.IsDir() {
		return reject(CodeUnsafePackageTree, "remove package file", "target is not a regular non-reparse file")
	}
	if err := os.Remove(full); err != nil {
		return rejectCause(CodeUnsafePackageTree, "remove package file", "exact file removal failed", err)
	}
	if _, err := os.Lstat(full); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return reject(CodeUnsafePackageTree, "remove package file", "target remained after removal")
		}
		return rejectCause(CodeUnsafePackageTree, "remove package file", "target absence could not be verified", err)
	}
	return nil
}

// Publish rechecks a verified temporary tree and renames it to its final path.
func (r *OutputRoot) Publish() error {
	if r == nil || r.destination == "" {
		return reject(CodeUnsafePath, "publish package", "output root is not a temporary publication root")
	}
	if r.complete {
		return reject(CodeUnsafePath, "publish package", "output root is already complete")
	}
	source := r.path
	destination := r.destination
	sourceParent := filepath.Dir(source)
	destinationParent := filepath.Dir(destination)
	relative, err := filepath.Rel(destinationParent, source)
	if err != nil || relative == "." || filepath.IsAbs(relative) || filepath.Dir(relative) != "." || filepath.Clean(sourceParent) != filepath.Clean(destinationParent) {
		return rejectCause(CodePathEscapesRoot, "publish package", "temporary package and destination are not direct siblings", err)
	}
	if err := ValidatePackageTree(source); err != nil {
		return fmt.Errorf("validate temporary package before publication: %w", err)
	}
	if err := ValidateOutputPath(destination); err != nil {
		return err
	}
	if err := ValidatePackageTree(source); err != nil {
		return fmt.Errorf("revalidate temporary package before publication: %w", err)
	}
	if err := ValidateOutputPath(destination); err != nil {
		return err
	}
	err = renameWithRetry(source, destination, platformRenameNoReplace, func() error {
		if err := ValidatePackageTree(source); err != nil {
			return fmt.Errorf("revalidate temporary package before retry: %w", err)
		}
		return ValidateOutputPath(destination)
	}, time.Sleep)
	if err != nil {
		if IsSafetyError(err) {
			return err
		}
		if inspectErr := ValidateOutputPath(destination); inspectErr != nil {
			return inspectErr
		}
		return rejectCause(CodeUnsafePath, "publish package", "temporary package could not be renamed to the final destination", err)
	}
	r.path = destination
	r.complete = true
	return nil
}

const maxRenameAttempts = 5

func renameWithRetry(source, destination string, rename func(string, string) error, revalidate func() error, sleep func(time.Duration)) error {
	delays := [...]time.Duration{
		10 * time.Millisecond,
		25 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
	}
	for attempt := 0; attempt < maxRenameAttempts; attempt++ {
		if err := rename(source, destination); err == nil {
			return nil
		} else if !platformIsRetryableRenameError(err) || attempt == maxRenameAttempts-1 {
			return err
		}
		sleep(delays[attempt])
		if err := revalidate(); err != nil {
			return err
		}
	}
	return reject(CodeUnsafePath, "publish package", "rename retry state was exhausted unexpectedly")
}

// Cleanup removes an incomplete tree only when it can still be proven safe.
func (r *OutputRoot) Cleanup() error {
	if r == nil || r.complete {
		return nil
	}
	if _, err := os.Lstat(r.path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return rejectCause(CodeUnsafePath, "clean incomplete output", "output root state cannot be determined", err)
	}
	if err := ValidatePackageTree(r.path); err != nil {
		return fmt.Errorf("refuse cleanup of an unsafe incomplete package: %w", err)
	}
	if err := os.RemoveAll(r.path); err != nil {
		return rejectCause(CodeUnsafePath, "clean incomplete output", "incomplete output could not be removed", err)
	}
	return nil
}

// WriteNewFile creates one root-relative file and revalidates the result.
func WriteNewFile(root, relative string, data []byte, perm fs.FileMode) error {
	full, err := prepareNewChild(root, relative, "write package file")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return rejectCause(CodeUnsafePath, "write package file", "new-file creation failed", err)
	}
	_, copyErr := io.Copy(file, bytes.NewReader(data))
	closeErr := file.Close()
	if copyErr != nil {
		return rejectCause(CodeUnsafePath, "write package file", "file data could not be written", copyErr)
	}
	if closeErr != nil {
		return rejectCause(CodeUnsafePath, "write package file", "file could not be closed", closeErr)
	}
	return validateExistingRegularFile(full, "revalidate package file")
}

// OpenFile opens one existing regular file after root, parent, and entry checks.
func OpenFile(root, relative string) (*os.File, error) {
	full, err := prepareExistingChild(root, relative, "open package file")
	if err != nil {
		return nil, err
	}
	if err := validateExistingRegularFile(full, "open package file"); err != nil {
		return nil, err
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, rejectCause(CodeUnsafePackageTree, "open package file", "package file could not be opened", err)
	}
	if err := validateExistingRegularFile(full, "revalidate opened package file"); err != nil {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, rejectCause(CodeUnsafePackageTree, "open package file", "opened handle is not a regular file", err)
	}
	return file, nil
}

// ReadFile reads one existing package-relative regular file through the guard.
func ReadFile(root, relative string) ([]byte, error) {
	file, err := OpenFile(root, relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, rejectCause(CodeUnsafePackageTree, "read package file", "package file could not be read", err)
	}
	return data, nil
}

// ValidatePackageTree rejects unsafe roots and every linked or non-regular entry.
func ValidatePackageTree(root string) error {
	abs, err := platformValidateAbsoluteLocalPath(root)
	if err != nil {
		return err
	}
	if err := validateExistingDirectory(abs, "validate package root"); err != nil {
		return err
	}
	err = filepath.WalkDir(abs, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return rejectCause(CodeUnsafePackageTree, "scan package tree", "package entry cannot be inspected", walkErr)
		}
		if err := platformValidateExistingPath(path); err != nil {
			return err
		}
		info, err := item.Info()
		if err != nil {
			return rejectCause(CodeUnsafePackageTree, "scan package tree", "package entry metadata cannot be read", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return reject(CodeReparsePointDetected, "scan package tree", "symbolic link or reparse point detected")
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return reject(CodeUnsafePackageTree, "scan package tree", "non-regular package entry detected")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ValidateRelativePath enforces canonical package-relative forward slashes.
func ValidateRelativePath(path string) error {
	if path == "" || strings.Contains(path, "\\") || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return reject(CodePathEscapesRoot, "validate child path", "path must use package-relative forward slashes")
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return reject(CodePathEscapesRoot, "validate child path", "path contains an unsafe component")
		}
		if err := platformValidateRelativeComponent(part); err != nil {
			return err
		}
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return reject(CodePathEscapesRoot, "validate child path", "path is not canonical")
	}
	return nil
}

func prepareNewChild(root, relative, operation string) (string, error) {
	full, err := prepareChild(root, relative, operation)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(full)
	if err := validateExistingDirectory(parent, operation); err != nil {
		return "", err
	}
	if info, err := os.Lstat(full); err == nil {
		if err := platformValidateExistingPath(full); err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", reject(CodeReparsePointDetected, operation, "child target is a link or reparse point")
		}
		return "", reject(CodeUnsafePath, operation, "child target already exists; overwrite is refused")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", rejectCause(CodeUnsafePath, operation, "child target state cannot be determined", err)
	}
	return full, nil
}

func prepareExistingChild(root, relative, operation string) (string, error) {
	full, err := prepareChild(root, relative, operation)
	if err != nil {
		return "", err
	}
	if err := validateExistingDirectory(filepath.Dir(full), operation); err != nil {
		return "", err
	}
	return full, nil
}

func prepareChild(root, relative, operation string) (string, error) {
	abs, err := platformValidateAbsoluteLocalPath(root)
	if err != nil {
		return "", err
	}
	if err := validateExistingDirectory(abs, operation); err != nil {
		return "", err
	}
	if err := ValidateRelativePath(relative); err != nil {
		return "", err
	}
	full := filepath.Join(abs, filepath.FromSlash(relative))
	observed, err := filepath.Rel(abs, full)
	if err != nil || observed == ".." || strings.HasPrefix(observed, ".."+string(filepath.Separator)) || filepath.IsAbs(observed) {
		return "", rejectCause(CodePathEscapesRoot, operation, "child path escapes the package root", err)
	}
	return full, nil
}

func validateExistingDirectory(path, operation string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return rejectCause(CodeUnsafePath, operation, "required directory cannot be inspected", err)
	}
	if err := platformValidateExistingPath(path); err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return reject(CodeUnsafePath, operation, "required path is not a real directory")
	}
	return nil
}

func validateExistingRegularFile(path, operation string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return rejectCause(CodeUnsafePackageTree, operation, "required file cannot be inspected", err)
	}
	if err := platformValidateExistingPath(path); err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return reject(CodeUnsafePackageTree, operation, "required path is not a regular file")
	}
	return nil
}
