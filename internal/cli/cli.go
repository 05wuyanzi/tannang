// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package cli implements the pre-alpha synthetic command surface.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/05wuyanzi/tannang/internal/application"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/integrity"
)

const (
	ExitOK            = 0
	ExitUsage         = 2
	ExitPartial       = 10
	ExitSkipped       = 11
	ExitBlocked       = 12
	ExitProviderError = 13
	ExitIntegrity     = 20
)

// Run executes one CLI request and returns its process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return ExitUsage
	}
	switch args[0] {
	case "collect":
		return runCollect(ctx, args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return ExitUsage
	}
}

func runCollect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("collect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixture := flags.String("synthetic", "", "embedded synthetic fixture name")
	output := flags.String("output", "", "new evidence package directory")
	if err := flags.Parse(args); err != nil {
		return ExitUsage
	}
	if flags.NArg() != 0 || *fixture == "" || *output == "" {
		fmt.Fprintln(stderr, "collect requires --synthetic <fixture> and --output <new-directory>")
		return ExitUsage
	}
	outcome, err := application.Collect(ctx, *fixture, *output)
	if err != nil {
		fmt.Fprintf(stderr, "collect failed: %v\n", err)
		return ExitProviderError
	}
	writeJSON(stdout, struct {
		PackagePath   string                       `json:"package_path"`
		Compatibility execution.CompatibilityState `json:"compatibility"`
		Execution     execution.State              `json:"execution"`
		Reason        execution.Reason             `json:"reason"`
	}{outcome.PackagePath, outcome.Record.Compatibility, outcome.Record.Execution.State, outcome.Record.Reason})
	switch outcome.Record.Execution.State {
	case execution.Collected:
		return ExitOK
	case execution.Partial:
		return ExitPartial
	case execution.Skipped:
		return ExitSkipped
	case execution.Blocked:
		return ExitBlocked
	default:
		return ExitProviderError
	}
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "verify requires exactly one evidence package path")
		return ExitUsage
	}
	if err := integrity.Verify(args[0]); err != nil {
		fmt.Fprintf(stderr, "verification failed: %v\n", err)
		return ExitIntegrity
	}
	writeJSON(stdout, struct {
		PackagePath string `json:"package_path"`
		Verified    bool   `json:"verified"`
	}{args[0], true})
	return ExitOK
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Tannang pre-alpha synthetic CLI")
	fmt.Fprintln(writer, "  tannang collect --synthetic <fixture> --output <new-directory>")
	fmt.Fprintln(writer, "  tannang verify <package-directory>")
}

func writeJSON(writer io.Writer, value any) {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
