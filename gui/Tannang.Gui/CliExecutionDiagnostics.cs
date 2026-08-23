// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Text;

namespace Tannang.Gui;

public enum ProcessStartFailureClass
{
    NONE,
    SIBLING_NOT_FOUND,
    INVALID_START_CONFIGURATION,
    START_RETURNED_FALSE,
    START_EXCEPTION
}

public enum DiagnosticClass
{
    NONE,
    PROCESS_START_FAILED,
    CLI_USAGE_OR_ARGUMENT_ERROR,
    CLI_EXITED_WITHOUT_FINAL_RESULT,
    CLI_OUTPUT_CONTRACT_INVALID,
    CLI_PROVIDER_OR_STARTUP_FAILURE,
    CLI_INTEGRITY_OR_PATH_FAILURE,
    CLI_INTERNAL_OUTPUT_FAILURE,
    UNKNOWN_CHILD_FAILURE
}

public sealed record CliExecutionDiagnostics
{
    public const int StdoutLengthCap = 16 * 1024;
    public const int OrdinaryStderrBytesObservedCap = 16 * 1024;
    public const int OrdinaryStderrLineCountCap = 16 * 1024;
    public const int RuntimeEventCountCap = 16 * 1024;
    public const int MalformedRuntimeSignalCountCap = 16 * 1024;

    public bool ProcessStarted { get; init; }
    public ProcessStartFailureClass ProcessStartFailureClass { get; init; }
    public int? ExitCode { get; init; }
    public bool StdoutPresent { get; init; }
    public int StdoutLength { get; init; }
    public bool ValidFinalJson { get; init; }
    public int RuntimeEventCount { get; init; }
    public int MalformedRuntimeSignalCount { get; init; }
    public int OrdinaryStderrLineCount { get; init; }
    public int OrdinaryStderrBytesObserved { get; init; }
    public long ProcessElapsedMilliseconds { get; init; }
    public DiagnosticClass DiagnosticClass { get; init; }

    public bool IsDiagnosticFailure => DiagnosticClass != DiagnosticClass.NONE;

    public string OperatorMessage => DiagnosticClass switch
    {
        DiagnosticClass.PROCESS_START_FAILED => "Tannang could not start the collector process.",
        DiagnosticClass.CLI_USAGE_OR_ARGUMENT_ERROR => "The collector rejected its startup request.",
        DiagnosticClass.CLI_EXITED_WITHOUT_FINAL_RESULT => "The collector exited before returning a valid final result.",
        DiagnosticClass.CLI_OUTPUT_CONTRACT_INVALID => "The collector returned an invalid final result.",
        DiagnosticClass.CLI_PROVIDER_OR_STARTUP_FAILURE => "The collector failed during startup or provider execution.",
        DiagnosticClass.CLI_INTEGRITY_OR_PATH_FAILURE => "The collector could not create or verify the Evidence Package.",
        DiagnosticClass.CLI_INTERNAL_OUTPUT_FAILURE => "The collector could not return its final result.",
        DiagnosticClass.UNKNOWN_CHILD_FAILURE => "The collector failed without a classifiable final result.",
        _ => string.Empty
    };

    public string ToTechnicalSummary()
    {
        string exitCode = ExitCode?.ToString() ?? "Unavailable";
        return $"{OperatorMessage} Process started: {YesNo(ProcessStarted)}; Exit code: {exitCode}; " +
               $"Final stdout present: {YesNo(StdoutPresent)}; Runtime events observed: {RuntimeEventCount}; " +
               $"Diagnostic class: {DiagnosticClass}.";
    }

    public string ToSafeClipboardText()
    {
        var builder = new StringBuilder();
        builder.AppendLine("Tannang child execution diagnostic");
        builder.AppendLine($"Process started: {ProcessStarted.ToString().ToLowerInvariant()}");
        builder.AppendLine($"Process start failure class: {ProcessStartFailureClass}");
        builder.AppendLine($"Exit code: {ExitCode?.ToString() ?? "unavailable"}");
        builder.AppendLine($"Final stdout present: {StdoutPresent.ToString().ToLowerInvariant()}");
        builder.AppendLine($"Final stdout length: {StdoutLength}");
        builder.AppendLine($"Final JSON valid: {ValidFinalJson.ToString().ToLowerInvariant()}");
        builder.AppendLine($"Runtime events observed: {RuntimeEventCount}");
        builder.AppendLine($"Malformed runtime signals: {MalformedRuntimeSignalCount}");
        builder.AppendLine($"Ordinary stderr lines: {OrdinaryStderrLineCount}");
        builder.AppendLine($"Ordinary stderr bytes observed: {OrdinaryStderrBytesObserved}");
        builder.AppendLine($"Elapsed milliseconds: {ProcessElapsedMilliseconds}");
        builder.Append($"Diagnostic class: {DiagnosticClass}");
        return builder.ToString();
    }

    public static int CapStdoutLength(int length) => Math.Min(Math.Max(length, 0), StdoutLengthCap);

    public static int SaturatingIncrement(int value, int cap)
    {
        return value >= cap ? cap : value + 1;
    }

    public static int SaturatingAdd(int value, int addition, int cap)
    {
        if (addition <= 0 || value >= cap)
        {
            return Math.Min(Math.Max(value, 0), cap);
        }

        return addition >= cap - value ? cap : value + addition;
    }

    private static string YesNo(bool value) => value ? "Yes" : "No";
}
