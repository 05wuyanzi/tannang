// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package integrity creates and verifies SHA-256 package manifests.
package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const ManifestPath = "hashes/manifest.json"

// Entry binds one package-relative file to its byte size and SHA-256.
type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// DirectoryEntry makes empty package directories explicit and verifiable.
type DirectoryEntry struct {
	Path            string `json:"path"`
	HashedFileCount int    `json:"hashed_file_count"`
}

// Manifest is intentionally self-excluding to avoid recursive hashing.
type Manifest struct {
	SchemaVersion string           `json:"schema_version"`
	Algorithm     string           `json:"algorithm"`
	SelfExcluded  string           `json:"self_excluded"`
	Directories   []DirectoryEntry `json:"directories"`
	Entries       []Entry          `json:"entries"`
}

// Generate writes a canonical, sorted SHA-256 manifest for package files.
func Generate(root string) error {
	observed, err := inventory(root)
	if err != nil {
		return err
	}
	manifest := Manifest{
		SchemaVersion: "1.0",
		Algorithm:     "SHA-256",
		SelfExcluded:  ManifestPath,
		Directories:   observed.Directories,
		Entries:       observed.Entries,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal integrity manifest: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(ManifestPath))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write integrity manifest: %w", err)
	}
	return nil
}

// Verify rejects missing, modified, extra, linked, or malformed files.
func Verify(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect package root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("package root must be a real directory")
	}

	manifestPath := filepath.Join(root, filepath.FromSlash(ManifestPath))
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return fmt.Errorf("integrity manifest is required: %w", err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return errors.New("integrity manifest must be a regular file")
	}
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	if manifest.SchemaVersion != "1.0" || manifest.Algorithm != "SHA-256" || manifest.SelfExcluded != ManifestPath {
		return errors.New("unsupported integrity manifest contract")
	}

	actual, err := inventory(root)
	if err != nil {
		return err
	}
	declaredDirectories := make(map[string]int, len(manifest.Directories))
	for _, directory := range manifest.Directories {
		if err := validateRelativePath(directory.Path); err != nil {
			return fmt.Errorf("invalid manifest directory %q: %w", directory.Path, err)
		}
		if directory.HashedFileCount < 0 {
			return fmt.Errorf("negative file count for directory %q", directory.Path)
		}
		if _, exists := declaredDirectories[directory.Path]; exists {
			return fmt.Errorf("duplicate manifest directory %q", directory.Path)
		}
		declaredDirectories[directory.Path] = directory.HashedFileCount
	}
	if len(declaredDirectories) != len(actual.Directories) {
		return fmt.Errorf("manifest directory count mismatch: declared %d, observed %d", len(declaredDirectories), len(actual.Directories))
	}
	for _, observed := range actual.Directories {
		expected, ok := declaredDirectories[observed.Path]
		if !ok {
			return fmt.Errorf("package directory is not declared: %s", observed.Path)
		}
		if expected != observed.HashedFileCount {
			return fmt.Errorf("file count mismatch for directory %s", observed.Path)
		}
	}
	declared := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if err := validateRelativePath(entry.Path); err != nil {
			return fmt.Errorf("invalid manifest entry %q: %w", entry.Path, err)
		}
		if _, exists := declared[entry.Path]; exists {
			return fmt.Errorf("duplicate manifest entry %q", entry.Path)
		}
		if len(entry.SHA256) != sha256.Size*2 {
			return fmt.Errorf("invalid SHA-256 length for %q", entry.Path)
		}
		if _, err := hex.DecodeString(entry.SHA256); err != nil {
			return fmt.Errorf("invalid SHA-256 for %q", entry.Path)
		}
		declared[entry.Path] = entry
	}
	if len(declared) != len(actual.Entries) {
		return fmt.Errorf("manifest file count mismatch: declared %d, observed %d", len(declared), len(actual.Entries))
	}
	for _, observed := range actual.Entries {
		expected, ok := declared[observed.Path]
		if !ok {
			return fmt.Errorf("package file is not declared: %s", observed.Path)
		}
		if expected.Size != observed.Size {
			return fmt.Errorf("size mismatch for %s", observed.Path)
		}
		if !strings.EqualFold(expected.SHA256, observed.SHA256) {
			return fmt.Errorf("SHA-256 mismatch for %s", observed.Path)
		}
	}
	return nil
}

func readManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open integrity manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode integrity manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("integrity manifest contains trailing JSON values")
		}
		return Manifest{}, fmt.Errorf("decode trailing integrity manifest data: %w", err)
	}
	return manifest, nil
}

type packageInventory struct {
	Directories []DirectoryEntry
	Entries     []Entry
}

func inventory(root string) (packageInventory, error) {
	entries := make([]Entry, 0)
	directoryCounts := make(map[string]int)
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if item.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not accepted in evidence packages: %s", path)
		}
		if item.IsDir() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			directoryCounts[filepath.ToSlash(relative)] = 0
			return nil
		}
		if !item.Type().IsRegular() {
			return fmt.Errorf("non-regular package file is not accepted: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ManifestPath {
			return nil
		}
		parent := filepath.ToSlash(filepath.Dir(relative))
		directoryCounts[parent]++
		entry, err := hashFile(path, relative)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return packageInventory{}, fmt.Errorf("inventory evidence package: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	directories := make([]DirectoryEntry, 0, len(directoryCounts))
	for path, count := range directoryCounts {
		directories = append(directories, DirectoryEntry{Path: path, HashedFileCount: count})
	}
	sort.Slice(directories, func(i, j int) bool { return directories[i].Path < directories[j].Path })
	return packageInventory{Directories: directories, Entries: entries}, nil
}

func hashFile(path, relative string) (Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return Entry{}, fmt.Errorf("open package file %s: %w", relative, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return Entry{}, fmt.Errorf("hash package file %s: %w", relative, err)
	}
	return Entry{Path: relative, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func validateRelativePath(path string) error {
	if path == "" || strings.Contains(path, "\\") || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return errors.New("path must use package-relative forward slashes")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("path contains an unsafe component")
		}
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return errors.New("path is not canonical")
	}
	if path == ManifestPath {
		return errors.New("manifest cannot declare itself")
	}
	return nil
}
