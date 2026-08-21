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
}

public static class ResultMapper
{
    public static MappedResult Map(CliProcessResult processResult)
    {
        if (!processResult.Started || processResult.ExitCode is null)
        {
            return Failed(processResult.FailureDetail ?? "The CLI process could not be started.");
        }

        CliSummary summary;
        try
        {
            summary = CliContractParser.Parse(processResult.Stdout);
        }
        catch (CliContractException exception)
        {
            return Failed(BoundedDetail(exception.Message));
        }

        switch (processResult.ExitCode.Value)
        {
            case 0:
                if (!string.Equals(summary.RunState, "COMPLETE", StringComparison.Ordinal))
                {
                    return Failed(ContractFailure(summary));
                }

                return summary.FinalizationVerified && summary.PackageReference.Length > 0
                    ? Success(summary.PackageReference)
                    : Failed(ContractFailure(summary));
            case 10 or 11:
                return string.Equals(summary.RunState, "PARTIAL", StringComparison.Ordinal)
                    ? Partial(summary)
                    : Failed(ExitRunStateFailure(processResult.ExitCode.Value, summary.RunState));
            case 12:
                return string.Equals(summary.RunState, "PARTIAL", StringComparison.Ordinal)
                    ? Blocked(summary)
                    : Failed(ExitRunStateFailure(processResult.ExitCode.Value, summary.RunState));
            default:
                return Failed(ExitFailure(processResult.ExitCode.Value, processResult.Stderr));
        }
    }

    private static MappedResult Success(string packageReference)
    {
        return new MappedResult
        {
            State = GuiResultState.Complete,
            Summary = "Collection complete.",
            PackageReference = packageReference
        };
    }

    private static MappedResult Partial(CliSummary summary)
    {
        return new MappedResult
        {
            State = GuiResultState.Partial,
            Summary = "Collection finished with partial evidence.",
            PackageReference = VerifiedPackage(summary)
        };
    }

    private static MappedResult Blocked(CliSummary summary)
    {
        return new MappedResult
        {
            State = GuiResultState.Blocked,
            Summary = "Collection was blocked.",
            PackageReference = VerifiedPackage(summary)
        };
    }

    private static MappedResult Failed(string detail)
    {
        return new MappedResult
        {
            State = GuiResultState.Failed,
            Summary = "Collection failed.",
            FailureDetail = BoundedDetail(detail)
        };
    }

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

    private static string ExitFailure(int exitCode, string stderr)
    {
        string detail = string.IsNullOrWhiteSpace(stderr)
            ? $"The CLI returned unexpected exit code {exitCode}."
            : stderr;
        return BoundedDetail(detail);
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
