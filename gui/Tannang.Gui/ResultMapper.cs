// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

namespace Tannang.Gui;

public enum GuiResultState
{
    Complete,
    Partial,
    Blocked,
    Failed
}

public sealed class MappedResult
{
    public required GuiResultState State { get; init; }
    public required string Summary { get; init; }
    public string? PackageReference { get; init; }
    public string? FailureDetail { get; init; }
    public CliExecutionDiagnostics? Diagnostics { get; init; }
}

public static class ResultMapper
{
    public static MappedResult Map(CliProcessResult processResult)
    {
        if (!processResult.Started)
        {
            CliExecutionDiagnostics diagnostics = processResult.Diagnostics with
            {
                ProcessStarted = false,
                ExitCode = null,
                ValidFinalJson = false,
                DiagnosticClass = DiagnosticClass.PROCESS_START_FAILED
            };
            return Failed(diagnostics.OperatorMessage, diagnostics);
        }

        if (processResult.ExitCode is null)
        {
            CliExecutionDiagnostics diagnostics = processResult.Diagnostics with
            {
                ProcessStarted = true,
                ProcessStartFailureClass = ProcessStartFailureClass.NONE,
                ExitCode = null,
                ValidFinalJson = false,
                DiagnosticClass = DiagnosticClass.UNKNOWN_CHILD_FAILURE
            };
            return Failed(diagnostics.OperatorMessage, diagnostics);
        }

        if (string.IsNullOrWhiteSpace(processResult.Stdout))
        {
            CliExecutionDiagnostics diagnostics = processResult.Diagnostics with
            {
                ProcessStarted = true,
                ProcessStartFailureClass = ProcessStartFailureClass.NONE,
                ExitCode = processResult.ExitCode,
                StdoutPresent = false,
                StdoutLength = CliExecutionDiagnostics.CapStdoutLength(processResult.Stdout.Length),
                ValidFinalJson = false,
                DiagnosticClass = ClassifyEmptyStdout(processResult.ExitCode.Value)
            };
            return Failed(diagnostics.OperatorMessage, diagnostics);
        }

        CliSummary summary;
        try
        {
            summary = CliContractParser.Parse(processResult.Stdout);
        }
        catch (CliContractException)
        {
            CliExecutionDiagnostics diagnostics = processResult.Diagnostics with
            {
                ProcessStarted = true,
                ProcessStartFailureClass = ProcessStartFailureClass.NONE,
                ExitCode = processResult.ExitCode,
                StdoutPresent = true,
                StdoutLength = CliExecutionDiagnostics.CapStdoutLength(processResult.Stdout.Length),
                ValidFinalJson = false,
                DiagnosticClass = DiagnosticClass.CLI_OUTPUT_CONTRACT_INVALID
            };
            return Failed(diagnostics.OperatorMessage, diagnostics);
        }

        CliExecutionDiagnostics validFinalDiagnostics = processResult.Diagnostics with
        {
            ProcessStarted = true,
            ProcessStartFailureClass = ProcessStartFailureClass.NONE,
            ExitCode = processResult.ExitCode,
            StdoutPresent = true,
            StdoutLength = CliExecutionDiagnostics.CapStdoutLength(processResult.Stdout.Length),
            ValidFinalJson = true,
            DiagnosticClass = DiagnosticClass.NONE
        };

        switch (processResult.ExitCode.Value)
        {
            case 0:
                if (!string.Equals(summary.RunState, "COMPLETE", StringComparison.Ordinal))
                {
                    return Failed(ContractFailure(summary), validFinalDiagnostics);
                }

                return summary.FinalizationVerified && summary.PackageReference.Length > 0
                    ? Success(summary.PackageReference, validFinalDiagnostics)
                    : Failed(ContractFailure(summary), validFinalDiagnostics);
            case 10 or 11:
                return string.Equals(summary.RunState, "PARTIAL", StringComparison.Ordinal)
                    ? Partial(summary, validFinalDiagnostics)
                    : Failed(ExitRunStateFailure(processResult.ExitCode.Value, summary.RunState), validFinalDiagnostics);
            case 12:
                return string.Equals(summary.RunState, "PARTIAL", StringComparison.Ordinal)
                    ? Blocked(summary, validFinalDiagnostics)
                    : Failed(ExitRunStateFailure(processResult.ExitCode.Value, summary.RunState), validFinalDiagnostics);
            default:
                return Failed("The collector returned an unexpected exit code.", validFinalDiagnostics);
        }
    }

    private static MappedResult Success(string packageReference, CliExecutionDiagnostics diagnostics)
    {
        return new MappedResult
        {
            State = GuiResultState.Complete,
            Summary = "Collection complete.",
            PackageReference = packageReference,
            Diagnostics = diagnostics
        };
    }

    private static MappedResult Partial(CliSummary summary, CliExecutionDiagnostics diagnostics)
    {
        return new MappedResult
        {
            State = GuiResultState.Partial,
            Summary = "Collection finished with partial evidence.",
            PackageReference = VerifiedPackage(summary),
            Diagnostics = diagnostics
        };
    }

    private static MappedResult Blocked(CliSummary summary, CliExecutionDiagnostics diagnostics)
    {
        return new MappedResult
        {
            State = GuiResultState.Blocked,
            Summary = "Collection was blocked.",
            PackageReference = VerifiedPackage(summary),
            Diagnostics = diagnostics
        };
    }

    private static MappedResult Failed(string detail, CliExecutionDiagnostics? diagnostics = null)
    {
        return new MappedResult
        {
            State = GuiResultState.Failed,
            Summary = "Collection failed.",
            FailureDetail = BoundedDetail(detail),
            Diagnostics = diagnostics
        };
    }

    private static DiagnosticClass ClassifyEmptyStdout(int exitCode) => exitCode switch
    {
        2 => DiagnosticClass.CLI_USAGE_OR_ARGUMENT_ERROR,
        13 => DiagnosticClass.CLI_PROVIDER_OR_STARTUP_FAILURE,
        14 => DiagnosticClass.CLI_INTERNAL_OUTPUT_FAILURE,
        20 or 21 => DiagnosticClass.CLI_INTEGRITY_OR_PATH_FAILURE,
        0 or 10 or 11 or 12 => DiagnosticClass.CLI_EXITED_WITHOUT_FINAL_RESULT,
        _ => DiagnosticClass.UNKNOWN_CHILD_FAILURE
    };

    private static string? VerifiedPackage(CliSummary summary)
    {
        return summary.FinalizationVerified && summary.PackageReference.Length > 0
            ? summary.PackageReference
            : null;
    }

    private static string ContractFailure(CliSummary summary)
    {
        if (!string.Equals(summary.RunState, "COMPLETE", StringComparison.Ordinal))
        {
            return "The CLI returned exit code 0 without a COMPLETE run state.";
        }

        if (!summary.FinalizationVerified)
        {
            return "The CLI returned exit code 0 without verified finalization.";
        }

        return "The CLI returned exit code 0 without a package reference.";
    }

    private static string ExitRunStateFailure(int exitCode, string runState)
    {
        return $"The CLI returned exit code {exitCode} with incompatible run state '{runState}'.";
    }

    private static string BoundedDetail(string value)
    {
        const int maxLength = 512;
        return value.Length <= maxLength ? value : value[..maxLength];
    }
}
