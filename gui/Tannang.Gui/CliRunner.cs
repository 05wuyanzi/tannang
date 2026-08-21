// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Diagnostics;

namespace Tannang.Gui;

public sealed class CliProcessResult
{
    public CliProcessResult(bool started, int? exitCode, string stdout, string stderr, string? failureDetail = null)
    {
        Started = started;
        ExitCode = exitCode;
        Stdout = stdout;
        Stderr = stderr;
        FailureDetail = failureDetail;
    }

    public bool Started { get; }
    public int? ExitCode { get; }
    public string Stdout { get; }
    public string Stderr { get; }
    public string? FailureDetail { get; }
}

public sealed class CliRunner
{
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

        return startInfo;
    }

    public async Task<CliProcessResult> RunAsync(string output, string? caseId)
    {
        ProcessStartInfo startInfo;
        try
        {
            startInfo = BuildStartInfo(output, caseId);
        }
        catch (Exception exception) when (exception is ArgumentException or FileNotFoundException or IOException)
        {
            return new CliProcessResult(false, null, string.Empty, string.Empty, BoundedDetail(exception.Message));
        }

        using var process = new Process { StartInfo = startInfo };
        try
        {
            if (!process.Start())
            {
                return new CliProcessResult(false, null, string.Empty, string.Empty, "The sibling tannang.exe could not be started.");
            }

            Task<string> stdoutTask = process.StandardOutput.ReadToEndAsync();
            Task<string> stderrTask = process.StandardError.ReadToEndAsync();
            await process.WaitForExitAsync().ConfigureAwait(true);
            string stdout = await stdoutTask.ConfigureAwait(true);
            string stderr = await stderrTask.ConfigureAwait(true);
            return new CliProcessResult(true, process.ExitCode, stdout, stderr);
        }
        catch (Exception exception) when (exception is InvalidOperationException or IOException or System.ComponentModel.Win32Exception)
        {
            return new CliProcessResult(false, null, string.Empty, string.Empty, BoundedDetail(exception.Message));
        }
    }

    private static string BoundedDetail(string value)
    {
        const int maxLength = 512;
        return value.Length <= maxLength ? value : value[..maxLength];
    }
}
