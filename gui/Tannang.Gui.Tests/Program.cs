// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Text.Json;
using System.Diagnostics;
using Tannang.Gui;

return TestRunner.Run();

internal static class TestRunner
{
    public static int Run()
    {
        var tests = new (string Name, Action Test)[]
        {
            ("complete result maps to COMPLETE", CompleteResultMapsToComplete),
            ("unverified exit zero fails closed", UnverifiedExitZeroFailsClosed),
            ("missing package reference fails closed", MissingPackageReferenceFailsClosed),
            ("partial exit ten maps to PARTIAL", PartialExitTenMapsToPartial),
            ("skipped exit eleven maps to PARTIAL", SkippedExitElevenMapsToPartial),
            ("blocked exit twelve maps to BLOCKED", BlockedExitMapsToBlocked),
            ("malformed JSON fails closed", MalformedJsonFailsClosed),
            ("extra non-JSON output fails closed", ExtraOutputFailsClosed),
            ("empty stdout fails closed", EmptyStdoutFailsClosed),
            ("complete JSON with failure exit fails closed", ContradictoryCompleteFailsClosed),
            ("duplicate finalization property fails closed", DuplicateFinalizationPropertyFailsClosed),
            ("duplicate package reference property fails closed", DuplicatePackageReferencePropertyFailsClosed),
            ("duplicate run state property fails closed", DuplicateRunStatePropertyFailsClosed),
            ("duplicate nested capability property fails closed", DuplicateNestedCapabilityPropertyFailsClosed),
            ("unknown run state fails closed for degraded exits", UnknownRunStateFailsClosedForDegradedExits),
            ("complete state fails closed for degraded exits", CompleteStateFailsClosedForDegradedExits),
            ("failed state fails closed for degraded exits", FailedStateFailsClosedForDegradedExits),
            ("process start failure maps to FAILED", ProcessStartFailureMapsToFailed),
            ("unexpected exit maps to FAILED", UnexpectedExitMapsToFailed),
            ("verified partial package path is retained", VerifiedPartialRetainsPackage),
            ("ArgumentList preserves output spaces", OutputArgumentPreservesSpaces),
            ("ArgumentList preserves case ID quotes", CaseIdArgumentPreservesQuotes),
            ("CLI resolution is sibling-only", SiblingResolutionIsStrict),
            ("duplicate start is rejected", DuplicateStartIsRejected)
        };

        int failures = 0;
        foreach ((string name, Action test) in tests)
        {
            try
            {
                test();
                Console.WriteLine($"PASS {name}");
            }
            catch (Exception exception)
            {
                failures++;
                Console.Error.WriteLine($"FAIL {name}: {exception.Message}");
            }
        }

        Console.WriteLine($"TESTS={tests.Length} FAILURES={failures}");
        return failures == 0 ? 0 : 1;
    }

    private static void CompleteResultMapsToComplete()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, SummaryJson("COMPLETE", true, @"C:\\package")));
        AssertEqual(GuiResultState.Complete, result.State);
        AssertEqual(@"C:\\package", result.PackageReference);
    }

    private static void UnverifiedExitZeroFailsClosed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, SummaryJson("COMPLETE", false, @"C:\\package")));
        AssertEqual(GuiResultState.Failed, result.State);
        AssertNull(result.PackageReference);
    }

    private static void MissingPackageReferenceFailsClosed()
    {
        string json = SummaryJson("COMPLETE", true, string.Empty, includePackageReference: false);
        MappedResult result = ResultMapper.Map(ProcessResult(0, json));
        AssertEqual(GuiResultState.Failed, result.State);
    }

    private static void PartialExitTenMapsToPartial()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(10, SummaryJson("PARTIAL", true, @"C:\\partial")));
        AssertEqual(GuiResultState.Partial, result.State);
    }

    private static void SkippedExitElevenMapsToPartial()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(11, SummaryJson("PARTIAL", false, string.Empty)));
        AssertEqual(GuiResultState.Partial, result.State);
        AssertNull(result.PackageReference);
    }

    private static void BlockedExitMapsToBlocked()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(12, SummaryJson("PARTIAL", true, @"C:\\blocked")));
        AssertEqual(GuiResultState.Blocked, result.State);
        AssertEqual(@"C:\\blocked", result.PackageReference);
    }

    private static void MalformedJsonFailsClosed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, "{not-json"));
        AssertEqual(GuiResultState.Failed, result.State);
    }

    private static void ExtraOutputFailsClosed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, SummaryJson("COMPLETE", true, @"C:\\package") + "\nextra"));
        AssertEqual(GuiResultState.Failed, result.State);
    }

    private static void EmptyStdoutFailsClosed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, string.Empty));
        AssertEqual(GuiResultState.Failed, result.State);
    }

    private static void ContradictoryCompleteFailsClosed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(13, SummaryJson("COMPLETE", true, @"C:\\package")));
        AssertEqual(GuiResultState.Failed, result.State);
        AssertNull(result.PackageReference);
    }

    private static void DuplicateFinalizationPropertyFailsClosed()
    {
        AssertParserFails("""
            {"collection_id":"COL-00000000-0000-4000-8000-000000000001","run_state":"COMPLETE","finalization_verified":false,"finalization_verified":true,"package_reference":"C:\\package","capabilities":[]}
            """);
    }

    private static void DuplicatePackageReferencePropertyFailsClosed()
    {
        AssertParserFails("""
            {"collection_id":"COL-00000000-0000-4000-8000-000000000001","run_state":"COMPLETE","finalization_verified":true,"package_reference":"C:\\first","package_reference":"C:\\second","capabilities":[]}
            """);
    }

    private static void DuplicateRunStatePropertyFailsClosed()
    {
        AssertParserFails("""
            {"collection_id":"COL-00000000-0000-4000-8000-000000000001","run_state":"PARTIAL","run_state":"COMPLETE","finalization_verified":true,"package_reference":"C:\\package","capabilities":[]}
            """);
    }

    private static void DuplicateNestedCapabilityPropertyFailsClosed()
    {
        AssertParserFails("""
            {"collection_id":"COL-00000000-0000-4000-8000-000000000001","run_state":"PARTIAL","finalization_verified":true,"package_reference":"C:\\package","capabilities":[{"id":"PROCESS_IDENTITY_SNAPSHOT","protected":true,"compatibility":"AVAILABLE","attempted":true,"execution_state":"COLLECTED","execution_state":"PARTIAL","execution_reason":"NONE"}]}
            """);
    }

    private static void UnknownRunStateFailsClosedForDegradedExits()
    {
        foreach (int exitCode in new[] { 10, 11, 12 })
        {
            MappedResult result = ResultMapper.Map(ProcessResult(exitCode, SummaryJson("unknown", true, @"C:\\package")));
            AssertEqual(GuiResultState.Failed, result.State);
        }

        AssertParserFails(SummaryJson("complete", true, @"C:\\package"));
    }

    private static void CompleteStateFailsClosedForDegradedExits()
    {
        foreach (int exitCode in new[] { 10, 11, 12 })
        {
            MappedResult result = ResultMapper.Map(ProcessResult(exitCode, SummaryJson("COMPLETE", true, @"C:\\package")));
            AssertEqual(GuiResultState.Failed, result.State);
            AssertNull(result.PackageReference);
        }
    }

    private static void FailedStateFailsClosedForDegradedExits()
    {
        foreach (int exitCode in new[] { 10, 11, 12 })
        {
            MappedResult result = ResultMapper.Map(ProcessResult(exitCode, SummaryJson("FAILED", true, @"C:\\package")));
            AssertEqual(GuiResultState.Failed, result.State);
            AssertNull(result.PackageReference);
        }
    }

    private static void ProcessStartFailureMapsToFailed()
    {
        MappedResult result = ResultMapper.Map(new CliProcessResult(false, null, string.Empty, string.Empty, "missing sibling"));
        AssertEqual(GuiResultState.Failed, result.State);
    }

    private static void UnexpectedExitMapsToFailed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(21, SummaryJson("PARTIAL", true, @"C:\\package")));
        AssertEqual(GuiResultState.Failed, result.State);
        AssertNull(result.PackageReference);
    }

    private static void VerifiedPartialRetainsPackage()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(10, SummaryJson("PARTIAL", true, @"C:\\partial")));
        AssertEqual(GuiResultState.Partial, result.State);
        AssertEqual(@"C:\\partial", result.PackageReference);
    }

    private static void OutputArgumentPreservesSpaces()
    {
        using TemporaryDirectory directory = new();
        string cliPath = Path.Combine(directory.Path, "tannang.exe");
        File.WriteAllBytes(cliPath, Array.Empty<byte>());
        string output = @"C:\Evidence Packages\case one";
        ProcessStartInfo info = CliRunner.BuildStartInfoForBaseDirectory(directory.Path, output, null);
        AssertEqual(output, info.ArgumentList[3]);
        AssertTrue(!info.UseShellExecute);
        AssertTrue(info.RedirectStandardOutput);
        AssertTrue(info.RedirectStandardError);
        AssertTrue(info.CreateNoWindow);
    }

    private static void CaseIdArgumentPreservesQuotes()
    {
        using TemporaryDirectory directory = new();
        File.WriteAllBytes(Path.Combine(directory.Path, "tannang.exe"), Array.Empty<byte>());
        string caseId = "case \"quoted\" value";
        ProcessStartInfo info = CliRunner.BuildStartInfoForBaseDirectory(directory.Path, @"C:\\output", caseId);
        AssertEqual("--case-id", info.ArgumentList[4]);
        AssertEqual(caseId, info.ArgumentList[5]);
    }

    private static void SiblingResolutionIsStrict()
    {
        using TemporaryDirectory directory = new();
        try
        {
            _ = CliRunner.ResolveCliPath(directory.Path);
            throw new InvalidOperationException("Expected missing sibling executable to fail.");
        }
        catch (FileNotFoundException)
        {
        }
    }

    private static void DuplicateStartIsRejected()
    {
        var guard = new RunStateGuard();
        AssertTrue(guard.TryEnter());
        AssertTrue(!guard.TryEnter());
        guard.Exit();
        AssertTrue(guard.TryEnter());
        guard.Exit();
    }

    private static CliProcessResult ProcessResult(int exitCode, string stdout)
    {
        return new CliProcessResult(true, exitCode, stdout, string.Empty);
    }

    private static string SummaryJson(string runState, bool finalizationVerified, string packageReference, bool includePackageReference = true)
    {
        var summary = new Dictionary<string, object?>
        {
            ["collection_id"] = "COL-00000000-0000-4000-8000-000000000001",
            ["run_state"] = runState,
            ["finalization_verified"] = finalizationVerified,
            ["capabilities"] = new[]
            {
                new Dictionary<string, object?>
                {
                    ["id"] = "PROCESS_IDENTITY_SNAPSHOT",
                    ["protected"] = true,
                    ["compatibility"] = "AVAILABLE",
                    ["attempted"] = true,
                    ["execution_state"] = "COLLECTED",
                    ["execution_reason"] = "NONE"
                }
            }
        };
        if (includePackageReference)
        {
            summary["package_reference"] = packageReference;
        }

        return JsonSerializer.Serialize(summary);
    }

    private static void AssertTrue(bool condition)
    {
        if (!condition)
        {
            throw new InvalidOperationException("Expected condition to be true.");
        }
    }

    private static void AssertNull(object? value)
    {
        if (value is not null)
        {
            throw new InvalidOperationException("Expected null value.");
        }
    }

    private static void AssertParserFails(string stdout)
    {
        try
        {
            _ = CliContractParser.Parse(stdout);
            throw new InvalidOperationException("Expected CLI contract parsing to fail.");
        }
        catch (CliContractException)
        {
        }
    }

    private static void AssertEqual<T>(T expected, T actual)
    {
        if (!EqualityComparer<T>.Default.Equals(expected, actual))
        {
            throw new InvalidOperationException($"Expected '{expected}', got '{actual}'.");
        }
    }

    private sealed class TemporaryDirectory : IDisposable
    {
        public TemporaryDirectory()
        {
            Path = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "tannang-gui-test-" + Guid.NewGuid().ToString("N"));
            Directory.CreateDirectory(Path);
        }

        public string Path { get; }

        public void Dispose()
        {
            if (Directory.Exists(Path))
            {
                Directory.Delete(Path, recursive: true);
            }
        }
    }
}
