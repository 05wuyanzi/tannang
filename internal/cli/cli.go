// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package cli implements the pre-alpha collection command surface.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/05wuyanzi/tannang/internal/application"
	"github.com/05wuyanzi/tannang/internal/buildinfo"
	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/evidence"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/pathsafe"
)

const realFirstStageFinalizationTimeout = 30 * time.Second

type realFirstStage interface {
	Run(context.Context, application.RunRequest) (application.RunResult, error)
}

type realFirstStageFactory func() (realFirstStage, error)
type realFirstStageRuntimeFactory func(application.RuntimeEventSink) (realFirstStage, error)

// newRealFirstStage is package-private so CLI tests can exercise terminal
// accounting without invoking the accepted Windows Provider.
var newRealFirstStage realFirstStageFactory = func() (realFirstStage, error) {
	return application.NewProcessIdentitySnapshotAndEventLogFirstStage(realFirstStageFinalizationTimeout)
}

var newRealFirstStageWithRuntimeSink realFirstStageRuntimeFactory = func(sink application.RuntimeEventSink) (realFirstStage, error) {
	return application.NewProcessIdentitySnapshotAndEventLogFirstStageWithRuntimeSink(realFirstStageFinalizationTimeout, sink)
}

const (
	ExitOK             = 0
	ExitUsage          = 2
	ExitPartial        = 10
	ExitSkipped        = 11
	ExitBlocked        = 12
	ExitProviderError  = 13
	exitTerminalOutput = 14
	ExitIntegrity      = 20
	ExitPathSafety     = 21
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
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return ExitUsage
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "version does not accept arguments")
		return ExitUsage
	}
	if err := writeJSON(stdout, buildinfo.Current()); err != nil {
		fmt.Fprintln(stderr, "failed to write command result")
		return exitTerminalOutput
	}
	return ExitOK
}

func runCollect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("collect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixture := flags.String("synthetic", "", "embedded synthetic fixture name")
	realSnapshot := flags.Bool("process-identity-snapshot", false, "collect the fixed Windows process identity snapshot")
	eventLogSystem := flags.Bool("windows-event-log-system", false, "collect the fixed local Windows System Event Log channel")
	hostIdentity := flags.Bool("windows-host-os-identity", false, "collect the fixed local Windows host/OS identity snapshot")
	output := flags.String("output", "", "new absolute local evidence package directory")
	caseID := flags.String("case-id", "", "optional case identifier for real collection")
	runtimeStatus := flags.Bool("runtime-status-stderr", false, "emit bounded runtime status records to stderr")
	if err := flags.Parse(args); err != nil {
		return ExitUsage
	}
	fixtureSelected := false
	realSnapshotSelected := false
	eventLogSelected := false
	hostIdentitySelected := false
	caseIDSelected := false
	runtimeStatusSelected := false
	flags.Visit(func(option *flag.Flag) {
		switch option.Name {
		case "synthetic":
			fixtureSelected = true
		case "process-identity-snapshot":
			realSnapshotSelected = true
		case "windows-event-log-system":
			eventLogSelected = true
		case "windows-host-os-identity":
			hostIdentitySelected = true
		case "case-id":
			caseIDSelected = true
		case "runtime-status-stderr":
			runtimeStatusSelected = true
		}
	})
	if flags.NArg() != 0 || *output == "" ||
		(fixtureSelected && (*fixture == "" || realSnapshotSelected || eventLogSelected || hostIdentitySelected || caseIDSelected || runtimeStatusSelected)) ||
		(realSnapshotSelected && !*realSnapshot) || (eventLogSelected && !*eventLogSystem) || (hostIdentitySelected && !*hostIdentity) || (runtimeStatusSelected && !*runtimeStatus) {
		fmt.Fprintln(stderr, "collect requires --output; the protected process identity baseline is the default, --process-identity-snapshot confirms that baseline, and --synthetic cannot be combined with real-only flags")
		return ExitUsage
	}
	if fixtureSelected {
		outcome, err := application.Collect(ctx, *fixture, *output)
		if err != nil {
			fmt.Fprintf(stderr, "collect failed: %v\n", err)
			if pathsafe.IsSafetyError(err) {
				return ExitPathSafety
			}
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
	// All three real flags are confirmation-only. The fixed production
	// baseline owns protected membership and merge/deduplication.
	return runRealCollect(ctx, *output, *caseID, nil, stdout, stderr, *runtimeStatus)
}

type realCollectCapabilitySummary struct {
	ID                string                       `json:"id"`
	Protected         bool                         `json:"protected"`
	Compatibility     execution.CompatibilityState `json:"compatibility"`
	Attempted         bool                         `json:"attempted"`
	ExecutionState    execution.State              `json:"execution_state"`
	ExecutionReason   execution.Reason             `json:"execution_reason"`
	MissingEvidence   []string                     `json:"missing_evidence,omitempty"`
	ReceiptReference  string                       `json:"receipt_reference,omitempty"`
	ArtifactReference string                       `json:"artifact_reference,omitempty"`
}

type realCollectSummary struct {
	CollectionID         string                          `json:"collection_id"`
	CaseID               string                          `json:"case_id,omitempty"`
	RunState             application.RunState            `json:"run_state"`
	OrchestrationReason  application.OrchestrationReason `json:"orchestration_reason,omitempty"`
	FinalizationVerified bool                            `json:"finalization_verified"`
	PackageReference     string                          `json:"package_reference,omitempty"`
	Capabilities         []realCollectCapabilitySummary  `json:"capabilities"`
}

func runRealCollect(ctx context.Context, output, caseID string, supplemental []capability.CapabilityRequest, stdout, stderr io.Writer, runtimeStatus bool) int {
	diagnosticWriter := stderr
	var arbiter *stderrArbiter
	if runtimeStatus {
		arbiter = newStderrArbiter(stderr)
		diagnosticWriter = arbiter
		defer arbiter.Stop()
	}
	request := application.RunRequest{CaseID: caseID, OutputDestination: output, Supplemental: supplemental}
	if err := request.Validate(); err != nil {
		fmt.Fprintln(diagnosticWriter, "invalid real collection request")
		return ExitUsage
	}
	if err := evidence.ValidateOutputPath(output); err != nil {
		fmt.Fprintf(diagnosticWriter, "collect failed: %v\n", err)
		return ExitPathSafety
	}
	var reporter *runtimeReporter
	if runtimeStatus {
		reporter = newRuntimeReporter(arbiter, runtimeHeartbeat, time.Now)
		defer reporter.Stop()
	}
	if (!runtimeStatus && newRealFirstStage == nil) || (runtimeStatus && newRealFirstStageWithRuntimeSink == nil) {
		fmt.Fprintln(diagnosticWriter, "real collection could not start")
		return ExitProviderError
	}
	var stage realFirstStage
	var err error
	if runtimeStatus {
		stage, err = newRealFirstStageWithRuntimeSink(reporter)
	} else {
		stage, err = newRealFirstStage()
	}
	if err != nil {
		fmt.Fprintln(diagnosticWriter, "real collection could not start")
		return ExitProviderError
	}
	if isNilRealFirstStage(stage) {
		fmt.Fprintln(diagnosticWriter, "real collection could not start")
		return ExitProviderError
	}
	result, err := stage.Run(ctx, request)
	if err != nil {
		fmt.Fprintln(diagnosticWriter, "real collection failed")
		return ExitProviderError
	}
	if err := writeJSON(stdout, summarizeRealCollect(result)); err != nil {
		fmt.Fprintln(diagnosticWriter, "failed to write command result")
		return exitTerminalOutput
	}
	return realCollectExitCode(result)
}

func isNilRealFirstStage(stage realFirstStage) bool {
	if stage == nil {
		return true
	}
	value := reflect.ValueOf(stage)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func summarizeRealCollect(result application.RunResult) realCollectSummary {
	summary := realCollectSummary{
		CollectionID:         result.Context.CollectionID,
		CaseID:               result.Context.CaseID,
		RunState:             result.State,
		OrchestrationReason:  result.OrchestrationReason,
		FinalizationVerified: result.FinalizationVerified,
		PackageReference:     result.PackageReference,
		Capabilities:         make([]realCollectCapabilitySummary, 0, len(result.Records)),
	}
	for _, record := range result.Records {
		summary.Capabilities = append(summary.Capabilities, realCollectCapabilitySummary{
			ID:                record.Request.ID,
			Protected:         record.Request.Protected,
			Compatibility:     record.Compatibility,
			Attempted:         record.Attempted,
			ExecutionState:    record.Execution.State,
			ExecutionReason:   record.Execution.Reason,
			MissingEvidence:   append([]string(nil), record.MissingEvidence...),
			ReceiptReference:  record.ReceiptReference,
			ArtifactReference: record.ArtifactReference,
		})
	}
	return summary
}

func realCollectExitCode(result application.RunResult) int {
	if result.OrchestrationReason == application.OrchestrationPackageFinalizationFailed {
		return ExitIntegrity
	}
	if result.OrchestrationReason == application.OrchestrationCancelled {
		return ExitPartial
	}
	if result.State == application.RunFailed {
		return ExitProviderError
	}
	if !result.FinalizationVerified {
		return ExitIntegrity
	}
	if len(result.Records) == 0 {
		return ExitProviderError
	}
	if result.State == application.RunComplete {
		return ExitOK
	}
	if result.State == application.RunPartial {
		for _, record := range result.Records {
			if record.Execution.State == execution.Failed {
				return ExitPartial
			}
		}
	}
	priority := ExitPartial
	for _, record := range result.Records {
		switch record.Execution.State {
		case execution.Failed:
			return ExitProviderError
		case execution.Blocked:
			priority = ExitBlocked
		case execution.Skipped:
			if priority == ExitPartial {
				priority = ExitSkipped
			}
		case execution.Partial:
			if priority == ExitSkipped {
				priority = ExitPartial
			}
		case execution.Collected:
		default:
			return ExitProviderError
		}
	}
	return priority
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "verify requires exactly one evidence package path")
		return ExitUsage
	}
	if err := integrity.Verify(args[0]); err != nil {
		fmt.Fprintf(stderr, "verification failed: %v\n", err)
		if pathsafe.IsSafetyError(err) {
			return ExitPathSafety
		}
		return ExitIntegrity
	}
	writeJSON(stdout, struct {
		PackagePath string `json:"package_path"`
		Verified    bool   `json:"verified"`
	}{args[0], true})
	return ExitOK
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Tannang pre-alpha CLI")
	fmt.Fprintln(writer, "  tannang collect --output <new-absolute-local-directory> [--case-id <case-id>] [--runtime-status-stderr]")
	fmt.Fprintln(writer, "  tannang collect --process-identity-snapshot --output <new-absolute-local-directory> [--case-id <case-id>] [--runtime-status-stderr]")
	fmt.Fprintln(writer, "  tannang collect --windows-event-log-system --output <new-absolute-local-directory> [--case-id <case-id>] [--runtime-status-stderr]")
	fmt.Fprintln(writer, "  tannang collect --windows-host-os-identity --output <new-absolute-local-directory> [--case-id <case-id>] [--runtime-status-stderr]")
	fmt.Fprintln(writer, "  tannang collect --synthetic <fixture> --output <new-absolute-local-directory>")
	fmt.Fprintln(writer, "  tannang verify <absolute-local-package-directory>")
	fmt.Fprintln(writer, "  tannang version")
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
