// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Diagnostics;
using System.Text;

namespace Tannang.Gui;

public sealed class CliProcessResult
{
    public CliProcessResult(
        bool started,
        int? exitCode,
        string stdout,
        string stderr,
        string? failureDetail = null,
        CliExecutionDiagnostics? diagnostics = null)
    {
        Started = started;
        ExitCode = exitCode;
        Stdout = stdout;
        Stderr = stderr;
        FailureDetail = failureDetail;
        Diagnostics = diagnostics ?? new CliExecutionDiagnostics
        {
            ProcessStarted = started,
            ProcessStartFailureClass = started ? ProcessStartFailureClass.NONE : ProcessStartFailureClass.START_EXCEPTION,
            ExitCode = exitCode,
            StdoutPresent = !string.IsNullOrWhiteSpace(stdout),
            StdoutLength = CliExecutionDiagnostics.CapStdoutLength(stdout.Length),
            OrdinaryStderrBytesObserved = Math.Min(Encoding.UTF8.GetByteCount(stderr), CliExecutionDiagnostics.OrdinaryStderrBytesObservedCap)
        };
    }

    public bool Started { get; }
    public int? ExitCode { get; }
    public string Stdout { get; }
    public string Stderr { get; }
    public string? FailureDetail { get; }
    public CliExecutionDiagnostics Diagnostics { get; }
}

internal delegate Task<CliProcessResult> ChildProcessExecution(
    string output,
    string? caseId,
    Action<RuntimeStatus>? onRuntimeStatus,
    Action? onMalformedRuntimeStatus,
    Action? onProcessExited);

public sealed class CliRunner
{
    public const string RuntimeStatusArgument = "--runtime-status-stderr";
    public const string MalformedRuntimeWarning = "Runtime status signal was malformed and was ignored.";
    private const int MaxDiagnosticCharacters = 16 * 1024;
    private readonly ChildProcessExecution _execution;

    public CliRunner() : this(ExecuteDefaultProcessAsync)
    {
    }

    internal CliRunner(ChildProcessExecution execution)
    {
        _execution = execution ?? throw new ArgumentNullException(nameof(execution));
    }

    internal CliRunner(string baseDirectory)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(baseDirectory);
        _execution = (output, caseId, onRuntimeStatus, onMalformedRuntimeStatus, onProcessExited) =>
            ExecuteProcessAsync(output, caseId, onRuntimeStatus, onMalformedRuntimeStatus, onProcessExited, baseDirectory, null);
    }

    internal CliRunner(string baseDirectory, Action<Process> afterStartForTest)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(baseDirectory);
        ArgumentNullException.ThrowIfNull(afterStartForTest);
        _execution = (output, caseId, onRuntimeStatus, onMalformedRuntimeStatus, onProcessExited) =>
            ExecuteProcessAsync(output, caseId, onRuntimeStatus, onMalformedRuntimeStatus, onProcessExited, baseDirectory, afterStartForTest);
    }

    public static string ResolveCliPath(string baseDirectory)
    {
        string path = Path.Combine(baseDirectory, "tannang.exe");
        if (!File.Exists(path))
        {
            throw new FileNotFoundException("The sibling tannang.exe was not found.", path);
        }

        return path;
    }

    public static ProcessStartInfo BuildStartInfo(string output, string? caseId)
    {
        return BuildStartInfoForBaseDirectory(AppContext.BaseDirectory, output, caseId);
    }

    public static ProcessStartInfo BuildStartInfoForBaseDirectory(string baseDirectory, string output, string? caseId)
    {
        if (string.IsNullOrWhiteSpace(output))
        {
            throw new ArgumentException("Output destination is required.", nameof(output));
        }

        string cliPath = ResolveCliPath(baseDirectory);
        var startInfo = new ProcessStartInfo
        {
            FileName = cliPath,
            UseShellExecute = false,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            CreateNoWindow = true
        };
        startInfo.ArgumentList.Add("collect");
        startInfo.ArgumentList.Add("--process-identity-snapshot");
        startInfo.ArgumentList.Add("--output");
        startInfo.ArgumentList.Add(output);
        if (!string.IsNullOrEmpty(caseId))
        {
            startInfo.ArgumentList.Add("--case-id");
            startInfo.ArgumentList.Add(caseId);
        }
        startInfo.ArgumentList.Add(RuntimeStatusArgument);

        return startInfo;
    }

    public Task<CliProcessResult> RunAsync(
        string output,
        string? caseId,
        Action<RuntimeStatus>? onRuntimeStatus = null,
        Action? onMalformedRuntimeStatus = null,
        Action? onProcessExited = null)
    {
        return _execution(output, caseId, onRuntimeStatus, onMalformedRuntimeStatus, onProcessExited);
    }

    private static Task<CliProcessResult> ExecuteDefaultProcessAsync(
        string output,
        string? caseId,
        Action<RuntimeStatus>? onRuntimeStatus,
        Action? onMalformedRuntimeStatus,
        Action? onProcessExited)
    {
        return ExecuteProcessAsync(output, caseId, onRuntimeStatus, onMalformedRuntimeStatus, onProcessExited, null, null);
    }

    private static async Task<CliProcessResult> ExecuteProcessAsync(
        string output,
        string? caseId,
        Action<RuntimeStatus>? onRuntimeStatus,
        Action? onMalformedRuntimeStatus,
        Action? onProcessExited,
        string? baseDirectory,
        Action<Process>? afterStartForTest)
    {
        var stopwatch = Stopwatch.StartNew();
        ProcessStartInfo startInfo;
        try
        {
            startInfo = baseDirectory is null
                ? BuildStartInfo(output, caseId)
                : BuildStartInfoForBaseDirectory(baseDirectory, output, caseId);
        }
        catch (Exception exception) when (exception is ArgumentException or FileNotFoundException or IOException)
        {
            ProcessStartFailureClass failureClass = exception is FileNotFoundException
                ? ProcessStartFailureClass.SIBLING_NOT_FOUND
                : exception is ArgumentException
                    ? ProcessStartFailureClass.INVALID_START_CONFIGURATION
                    : ProcessStartFailureClass.START_EXCEPTION;
            return CreateFailureResult(false, null, string.Empty, string.Empty, BoundedDetail(exception.Message), failureClass, stopwatch.ElapsedMilliseconds);
        }

        using var process = new Process { StartInfo = startInfo };
        bool processStarted = false;
        try
        {
            if (!process.Start())
            {
                return CreateFailureResult(false, null, string.Empty, string.Empty, "The sibling tannang.exe could not be started.", ProcessStartFailureClass.START_RETURNED_FALSE, stopwatch.ElapsedMilliseconds);
            }
            processStarted = true;
            afterStartForTest?.Invoke(process);

            Task<string> stdoutTask = process.StandardOutput.ReadToEndAsync();
            Task<StderrCapture> stderrTask = ReadStandardErrorAsync(process.StandardError, onRuntimeStatus, onMalformedRuntimeStatus);
            await process.WaitForExitAsync().ConfigureAwait(true);
            try
            {
                onProcessExited?.Invoke();
            }
            catch
            {
                // Runtime UI observation cannot affect collection result handling.
            }
            string stdout = await stdoutTask.ConfigureAwait(true);
            StderrCapture stderr = await stderrTask.ConfigureAwait(true);
            return new CliProcessResult(true, process.ExitCode, stdout, stderr.Diagnostics, diagnostics: new CliExecutionDiagnostics
            {
                ProcessStarted = true,
                ProcessStartFailureClass = ProcessStartFailureClass.NONE,
                ExitCode = process.ExitCode,
                StdoutPresent = !string.IsNullOrWhiteSpace(stdout),
                StdoutLength = CliExecutionDiagnostics.CapStdoutLength(stdout.Length),
                RuntimeEventCount = stderr.RuntimeEventCount,
                MalformedRuntimeSignalCount = stderr.MalformedRuntimeSignalCount,
                OrdinaryStderrLineCount = stderr.OrdinaryStderrLineCount,
                OrdinaryStderrBytesObserved = stderr.OrdinaryStderrBytesObserved,
                ProcessElapsedMilliseconds = stopwatch.ElapsedMilliseconds
            });
        }
        catch (Exception exception) when (exception is InvalidOperationException or IOException or System.ComponentModel.Win32Exception)
        {
            int? exitCode = processStarted ? TryGetExitCode(process) : null;
            return CreateFailureResult(processStarted, exitCode, string.Empty, string.Empty, BoundedDetail(exception.Message),
                processStarted ? ProcessStartFailureClass.NONE : ProcessStartFailureClass.START_EXCEPTION,
                stopwatch.ElapsedMilliseconds);
        }
    }

    private static CliProcessResult CreateFailureResult(
        bool processStarted,
        int? exitCode,
        string stdout,
        string stderr,
        string failureDetail,
        ProcessStartFailureClass startFailureClass,
        long elapsedMilliseconds)
    {
        return new CliProcessResult(processStarted, exitCode, stdout, stderr, failureDetail, new CliExecutionDiagnostics
        {
            ProcessStarted = processStarted,
            ProcessStartFailureClass = startFailureClass,
            ExitCode = exitCode,
            StdoutPresent = !string.IsNullOrWhiteSpace(stdout),
            StdoutLength = CliExecutionDiagnostics.CapStdoutLength(stdout.Length),
            ProcessElapsedMilliseconds = elapsedMilliseconds
        });
    }

    private static int? TryGetExitCode(Process process)
    {
        try
        {
            return process.HasExited ? process.ExitCode : null;
        }
        catch
        {
            return null;
        }
    }

    private sealed record StderrCapture(
        string Diagnostics,
        int RuntimeEventCount,
        int MalformedRuntimeSignalCount,
        int OrdinaryStderrLineCount,
        int OrdinaryStderrBytesObserved);

    private static async Task<StderrCapture> ReadStandardErrorAsync(
        StreamReader reader,
        Action<RuntimeStatus>? onRuntimeStatus,
        Action? onMalformedRuntimeStatus)
    {
        var diagnostics = new StringBuilder();
        int runtimeEventCount = 0;
        int malformedRuntimeSignalCount = 0;
        int ordinaryStderrLineCount = 0;
        int ordinaryStderrBytesObserved = 0;
        string? line;
        while ((line = await reader.ReadLineAsync().ConfigureAwait(false)) is not null)
        {
            if (line.StartsWith(RuntimeStatus.Prefix, StringComparison.Ordinal))
            {
                try
                {
                    RuntimeStatus status = RuntimeStatusParser.Parse(line);
                    runtimeEventCount = CliExecutionDiagnostics.SaturatingIncrement(runtimeEventCount, CliExecutionDiagnostics.RuntimeEventCountCap);
                    try
                    {
                        onRuntimeStatus?.Invoke(status);
                    }
                    catch
                    {
                        // Runtime UI observation cannot affect collection result handling.
                    }
                }
                catch (RuntimeStatusException)
                {
                    malformedRuntimeSignalCount = CliExecutionDiagnostics.SaturatingIncrement(malformedRuntimeSignalCount, CliExecutionDiagnostics.MalformedRuntimeSignalCountCap);
                    try
                    {
                        onMalformedRuntimeStatus?.Invoke();
                    }
                    catch
                    {
                        // Runtime UI observation cannot affect collection result handling.
                    }
                }
                continue;
            }

            ordinaryStderrLineCount = CliExecutionDiagnostics.SaturatingIncrement(ordinaryStderrLineCount, CliExecutionDiagnostics.OrdinaryStderrLineCountCap);
            ordinaryStderrBytesObserved = CliExecutionDiagnostics.SaturatingAdd(
                ordinaryStderrBytesObserved,
                Encoding.UTF8.GetByteCount(line),
                CliExecutionDiagnostics.OrdinaryStderrBytesObservedCap);
            AppendBoundedDiagnostic(diagnostics, line);
        }
        return new StderrCapture(diagnostics.ToString(), runtimeEventCount, malformedRuntimeSignalCount, ordinaryStderrLineCount, ordinaryStderrBytesObserved);
    }

    private static void AppendBoundedDiagnostic(StringBuilder diagnostics, string line)
    {
        const string separator = "\n";
        int remaining = MaxDiagnosticCharacters - diagnostics.Length;
        if (remaining <= 0)
        {
            return;
        }
        if (diagnostics.Length > 0)
        {
            diagnostics.Append(separator);
            remaining--;
        }
        if (remaining > 0)
        {
            diagnostics.Append(line.AsSpan(0, Math.Min(line.Length, remaining)));
        }
    }

    private static string BoundedDetail(string value)
    {
        const int maxLength = 512;
        return value.Length <= maxLength ? value : value[..maxLength];
    }
}
