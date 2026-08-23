// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Text.Json;
using System.Diagnostics;
using System.Reflection;
using System.Text;
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
            ("process start failure diagnostics are safe", ProcessStartFailureDiagnosticsAreSafe),
            ("controlled start exception reaches safe mapper", ControlledStartExceptionReachesSafeMapper),
            ("controlled start false reaches safe mapper", ControlledStartFalseReachesSafeMapper),
            ("started child with unavailable exit remains started", StartedChildWithUnavailableExitRemainsStarted),
            ("real process start failure reaches production runner", RealProcessStartFailureReachesProductionRunner),
            ("unexpected exit maps to FAILED", UnexpectedExitMapsToFailed),
            ("empty stdout exit codes classify safely", EmptyStdoutExitCodesClassifySafely),
            ("malformed stdout gets contract diagnostic", MalformedStdoutGetsContractDiagnostic),
            ("valid JSON semantic failure has no early diagnostic", ValidJsonSemanticFailureHasNoEarlyDiagnostic),
            ("valid final JSON metadata is true for mapped results", ValidFinalJsonMetadataIsTrueForMappedResults),
            ("diagnostics preserve fast exit metadata", DiagnosticsPreserveFastExitMetadata),
            ("diagnostic counters saturate", DiagnosticCountersSaturate),
            ("copy diagnostics use safe metadata", CopyDiagnosticsUseSafeMetadata),
            ("verified partial package path is retained", VerifiedPartialRetainsPackage),
            ("ArgumentList preserves output spaces", OutputArgumentPreservesSpaces),
            ("ArgumentList preserves case ID quotes", CaseIdArgumentPreservesQuotes),
            ("CLI resolution is sibling-only", SiblingResolutionIsStrict),
            ("duplicate start is rejected", DuplicateStartIsRejected),
            ("GUI child arguments opt in to runtime status", RuntimeStatusArgumentIsIncluded),
            ("valid runtime status parses", ValidRuntimeStatusParses),
            ("runtime status accepts unique extensions", RuntimeStatusAcceptsUniqueExtensions),
            ("duplicate runtime property is rejected", DuplicateRuntimePropertyIsRejected),
            ("nested duplicate runtime property is rejected", NestedDuplicateRuntimePropertyIsRejected),
            ("unknown runtime pair is rejected", UnknownRuntimePairIsRejected),
            ("invalid runtime pair is rejected", InvalidRuntimePairIsRejected),
            ("bad runtime timestamp is rejected", BadRuntimeTimestampIsRejected),
            ("trailing runtime JSON is rejected", TrailingRuntimeJsonIsRejected),
            ("oversized runtime record is rejected", OversizedRuntimeRecordIsRejected),
            ("heartbeat and activity are tracked separately", HeartbeatAndActivityAreTrackedSeparately),
            ("quiet runtime state is responsive", QuietRuntimeStateIsResponsive),
            ("missing runtime signal degrades", MissingRuntimeSignalDegrades),
            ("missing runtime signal becomes uncertain", MissingRuntimeSignalBecomesUncertain),
            ("runtime signal recovers", RuntimeSignalRecovers),
            ("fresh heartbeat and activity are active", FreshHeartbeatAndActivityAreActive),
            ("fresh heartbeat with quiet activity is responsive", FreshHeartbeatWithQuietActivityIsResponsive),
            ("activity cannot mask stale heartbeat", ActivityCannotMaskStaleHeartbeat),
            ("activity cannot mask uncertain heartbeat", ActivityCannotMaskUncertainHeartbeat),
            ("activity without heartbeat degrades", ActivityWithoutHeartbeatDegrades),
            ("activity without heartbeat becomes uncertain", ActivityWithoutHeartbeatBecomesUncertain),
            ("heartbeat recovery with activity is active", HeartbeatRecoveryWithActivityIsActive),
            ("heartbeat recovery with quiet activity is responsive", HeartbeatRecoveryWithQuietActivityIsResponsive),
            ("process lifecycle states do not regress", ProcessLifecycleStatesDoNotRegress),
            ("runtime state labels preserve frozen names", RuntimeStateLabelsPreserveFrozenNames),
            ("runtime terminal event is not final authority", RuntimeTerminalEventIsNotFinalAuthority),
            ("malformed runtime warning is fixed", MalformedRuntimeWarningIsFixed),
            ("stderr runtime routing hides malformed raw input", StderrRuntimeRoutingHidesMalformedRawInput),
            ("live log enforces line bound and marker", LiveLogEnforcesLineBoundAndMarker),
            ("live log enforces byte bound", LiveLogEnforcesByteBound),
            ("follow tail pauses and resumes", FollowTailPausesAndResumes)
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

    private static void ProcessStartFailureDiagnosticsAreSafe()
    {
        const string secret = "START-EXCEPTION-SECRET";
        MappedResult result = ResultMapper.Map(new CliProcessResult(false, null, string.Empty, string.Empty, secret));
        AssertEqual(DiagnosticClass.PROCESS_START_FAILED, result.Diagnostics?.DiagnosticClass);
        AssertTrue(!result.FailureDetail!.Contains(secret, StringComparison.Ordinal));
        AssertTrue(!result.Diagnostics!.ToSafeClipboardText().Contains(secret, StringComparison.Ordinal));
    }

    private static void ControlledStartExceptionReachesSafeMapper()
    {
        const string secret = "CONTROLLED-START-EXCEPTION";
        CliRunner runner = CreateControlledRunner(new CliProcessResult(false, null, string.Empty, string.Empty, secret));
        CliProcessResult processResult = runner.RunAsync(@"C:\output", null).GetAwaiter().GetResult();
        MappedResult result = ResultMapper.Map(processResult);
        AssertEqual(DiagnosticClass.PROCESS_START_FAILED, result.Diagnostics?.DiagnosticClass);
        AssertTrue(!result.FailureDetail!.Contains(secret, StringComparison.Ordinal));
    }

    private static void ControlledStartFalseReachesSafeMapper()
    {
        CliRunner runner = CreateControlledRunner(new CliProcessResult(false, null, string.Empty, string.Empty, "returned false"));
        CliProcessResult processResult = runner.RunAsync(@"C:\output", null).GetAwaiter().GetResult();
        MappedResult result = ResultMapper.Map(processResult);
        AssertEqual(DiagnosticClass.PROCESS_START_FAILED, result.Diagnostics?.DiagnosticClass);
        AssertEqual((int?)null, result.Diagnostics?.ExitCode);
    }

    private static void StartedChildWithUnavailableExitRemainsStarted()
    {
        using TemporaryDirectory directory = new();
        string comSpec = Environment.GetEnvironmentVariable("ComSpec")
            ?? throw new InvalidOperationException("ComSpec is unavailable on this Windows test host.");
        File.Copy(comSpec, Path.Combine(directory.Path, "tannang.exe"));
        CliProcessResult processResult = CreatePostStartFailureRunner(directory.Path)
            .RunAsync(@"C:\output", null)
            .GetAwaiter()
            .GetResult();
        AssertEqual(true, processResult.Started);
        AssertEqual((int?)null, processResult.ExitCode);
        AssertEqual(ProcessStartFailureClass.NONE, processResult.Diagnostics.ProcessStartFailureClass);
        MappedResult result = ResultMapper.Map(processResult);
        AssertEqual(GuiResultState.Failed, result.State);
        AssertEqual(true, result.Diagnostics?.ProcessStarted ?? false);
        AssertEqual((int?)null, result.Diagnostics?.ExitCode);
        AssertEqual(ProcessStartFailureClass.NONE, result.Diagnostics?.ProcessStartFailureClass);
        AssertEqual(DiagnosticClass.UNKNOWN_CHILD_FAILURE, result.Diagnostics?.DiagnosticClass);
        AssertTrue(result.Diagnostics!.ToTechnicalSummary().Contains("Process started: Yes", StringComparison.Ordinal));
        AssertTrue(result.Diagnostics.ToTechnicalSummary().Contains("Exit code: Unavailable", StringComparison.Ordinal));
    }

    private static void RealProcessStartFailureReachesProductionRunner()
    {
        using TemporaryDirectory directory = new();
        File.WriteAllText(Path.Combine(directory.Path, "tannang.exe"), "not a Windows executable");
        CliProcessResult processResult = CreateBaseDirectoryRunner(directory.Path)
            .RunAsync(@"C:\output", null)
            .GetAwaiter()
            .GetResult();
        AssertEqual(false, processResult.Started);
        AssertEqual((int?)null, processResult.ExitCode);
        AssertTrue(processResult.Diagnostics.ProcessStartFailureClass is
            ProcessStartFailureClass.START_EXCEPTION or ProcessStartFailureClass.START_RETURNED_FALSE);
        MappedResult result = ResultMapper.Map(processResult);
        AssertEqual(DiagnosticClass.PROCESS_START_FAILED, result.Diagnostics?.DiagnosticClass);
        AssertTrue(!result.FailureDetail!.Contains("not a Windows executable", StringComparison.Ordinal));
    }

    private static void UnexpectedExitMapsToFailed()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(21, SummaryJson("PARTIAL", true, @"C:\\package")));
        AssertEqual(GuiResultState.Failed, result.State);
        AssertNull(result.PackageReference);
    }

    private static void EmptyStdoutExitCodesClassifySafely()
    {
        foreach ((int exitCode, DiagnosticClass expected) in new[]
        {
            (0, DiagnosticClass.CLI_EXITED_WITHOUT_FINAL_RESULT),
            (2, DiagnosticClass.CLI_USAGE_OR_ARGUMENT_ERROR),
            (10, DiagnosticClass.CLI_EXITED_WITHOUT_FINAL_RESULT),
            (11, DiagnosticClass.CLI_EXITED_WITHOUT_FINAL_RESULT),
            (12, DiagnosticClass.CLI_EXITED_WITHOUT_FINAL_RESULT),
            (13, DiagnosticClass.CLI_PROVIDER_OR_STARTUP_FAILURE),
            (14, DiagnosticClass.CLI_INTERNAL_OUTPUT_FAILURE),
            (20, DiagnosticClass.CLI_INTEGRITY_OR_PATH_FAILURE),
            (21, DiagnosticClass.CLI_INTEGRITY_OR_PATH_FAILURE),
            (99, DiagnosticClass.UNKNOWN_CHILD_FAILURE)
        })
        {
            MappedResult result = ResultMapper.Map(ProcessResult(exitCode, string.Empty));
            AssertEqual(GuiResultState.Failed, result.State);
            AssertEqual(expected, result.Diagnostics?.DiagnosticClass);
            AssertNull(result.PackageReference);
        }
    }

    private static void MalformedStdoutGetsContractDiagnostic()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, "{not-json"));
        AssertEqual(GuiResultState.Failed, result.State);
        AssertEqual(DiagnosticClass.CLI_OUTPUT_CONTRACT_INVALID, result.Diagnostics?.DiagnosticClass);
        AssertEqual(false, result.Diagnostics?.ValidFinalJson ?? true);
        AssertTrue(!result.FailureDetail!.Contains("{not-json", StringComparison.Ordinal));
    }

    private static void ValidJsonSemanticFailureHasNoEarlyDiagnostic()
    {
        MappedResult result = ResultMapper.Map(ProcessResult(0, SummaryJson("COMPLETE", false, @"C:\package")));
        AssertEqual(GuiResultState.Failed, result.State);
        AssertEqual(true, result.Diagnostics?.ValidFinalJson ?? false);
        AssertEqual(DiagnosticClass.NONE, result.Diagnostics?.DiagnosticClass);
    }

    private static void ValidFinalJsonMetadataIsTrueForMappedResults()
    {
        (int ExitCode, string RunState)[] cases =
        {
            (0, "COMPLETE"),
            (10, "PARTIAL"),
            (12, "PARTIAL"),
            (13, "FAILED")
        };
        foreach ((int exitCode, string runState) in cases)
        {
            MappedResult result = ResultMapper.Map(ProcessResult(exitCode, SummaryJson(runState, true, @"C:\package")));
            AssertEqual(true, result.Diagnostics?.ValidFinalJson ?? false);
            AssertEqual(DiagnosticClass.NONE, result.Diagnostics?.DiagnosticClass);
        }
    }

    private static void DiagnosticsPreserveFastExitMetadata()
    {
        var diagnostics = new CliExecutionDiagnostics
        {
            ProcessStarted = true,
            ProcessStartFailureClass = ProcessStartFailureClass.NONE,
            ExitCode = 13,
            StdoutPresent = false,
            StdoutLength = 0,
            RuntimeEventCount = 2,
            MalformedRuntimeSignalCount = 1,
            OrdinaryStderrLineCount = 3,
            OrdinaryStderrBytesObserved = 12,
            ProcessElapsedMilliseconds = 4
        };
        MappedResult result = ResultMapper.Map(new CliProcessResult(true, 13, string.Empty, "diagnostic", diagnostics: diagnostics));
        AssertEqual(true, result.Diagnostics?.ProcessStarted ?? false);
        AssertEqual((int?)13, result.Diagnostics?.ExitCode);
        AssertEqual(false, result.Diagnostics?.StdoutPresent ?? true);
        AssertEqual(2, result.Diagnostics?.RuntimeEventCount ?? -1);
        AssertEqual(1, result.Diagnostics?.MalformedRuntimeSignalCount ?? -1);
        AssertEqual(4L, result.Diagnostics?.ProcessElapsedMilliseconds ?? -1L);
    }

    private static void DiagnosticCountersSaturate()
    {
        AssertEqual(CliExecutionDiagnostics.RuntimeEventCountCap,
            CliExecutionDiagnostics.SaturatingIncrement(CliExecutionDiagnostics.RuntimeEventCountCap, CliExecutionDiagnostics.RuntimeEventCountCap));
        AssertEqual(CliExecutionDiagnostics.OrdinaryStderrBytesObservedCap,
            CliExecutionDiagnostics.SaturatingAdd(CliExecutionDiagnostics.OrdinaryStderrBytesObservedCap - 1, 100, CliExecutionDiagnostics.OrdinaryStderrBytesObservedCap));
        AssertEqual(CliExecutionDiagnostics.StdoutLengthCap,
            CliExecutionDiagnostics.CapStdoutLength(CliExecutionDiagnostics.StdoutLengthCap + 1));
    }

    private static void CopyDiagnosticsUseSafeMetadata()
    {
        const string secret = "RAW-SECRET-DO-NOT-COPY";
        var diagnostics = new CliExecutionDiagnostics
        {
            ProcessStarted = true,
            ProcessStartFailureClass = ProcessStartFailureClass.NONE,
            ExitCode = 13,
            StdoutPresent = false,
            StdoutLength = 0,
            ValidFinalJson = false,
            RuntimeEventCount = 0,
            MalformedRuntimeSignalCount = 0,
            OrdinaryStderrLineCount = 1,
            OrdinaryStderrBytesObserved = 10,
            ProcessElapsedMilliseconds = 5,
            DiagnosticClass = DiagnosticClass.CLI_PROVIDER_OR_STARTUP_FAILURE
        };
        string clipboardText = diagnostics.ToSafeClipboardText();
        AssertTrue(clipboardText.Contains("Diagnostic class: CLI_PROVIDER_OR_STARTUP_FAILURE", StringComparison.Ordinal));
        AssertTrue(!clipboardText.Contains(secret, StringComparison.Ordinal));
        AssertTrue(!clipboardText.Contains("Output destination", StringComparison.Ordinal));
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

    private static void RuntimeStatusArgumentIsIncluded()
    {
        using TemporaryDirectory directory = new();
        File.WriteAllBytes(Path.Combine(directory.Path, "tannang.exe"), Array.Empty<byte>());
        ProcessStartInfo info = CliRunner.BuildStartInfoForBaseDirectory(directory.Path, @"C:\output", "CASE-01");
        AssertEqual(CliRunner.RuntimeStatusArgument, info.ArgumentList[^1]);
        AssertEqual(1, info.ArgumentList.Count(value => value == CliRunner.RuntimeStatusArgument));
    }

    private static void ValidRuntimeStatusParses()
    {
        RuntimeStatus status = RuntimeStatusParser.Parse(RuntimeLine("PHASE", "FINGERPRINT"));
        AssertEqual("PHASE", status.Type);
        AssertEqual("FINGERPRINT", status.Event);
        AssertEqual("Preparing target fingerprint", status.OperatorText);
        AssertEqual(TimeSpan.Zero, status.At.Offset);
        RuntimeStatus nanosecond = RuntimeStatusParser.Parse(
            RuntimeStatus.Prefix + "{\"type\":\"HEARTBEAT\",\"event\":\"PULSE\",\"at\":\"2026-08-21T01:02:03.123456789Z\"}");
        AssertTrue(nanosecond.IsHeartbeat);
    }

    private static void RuntimeStatusAcceptsUniqueExtensions()
    {
        RuntimeStatus status = RuntimeStatusParser.Parse(
            RuntimeStatus.Prefix + "{\"type\":\"HEARTBEAT\",\"event\":\"PULSE\",\"at\":\"2026-08-21T01:02:03Z\",\"extension\":{\"value\":1}}");
        AssertTrue(status.IsHeartbeat);
    }

    private static void DuplicateRuntimePropertyIsRejected()
    {
        AssertRuntimeParserFails(
            RuntimeStatus.Prefix + "{\"type\":\"PHASE\",\"type\":\"HEARTBEAT\",\"event\":\"PULSE\",\"at\":\"2026-08-21T01:02:03Z\"}");
    }

    private static void NestedDuplicateRuntimePropertyIsRejected()
    {
        AssertRuntimeParserFails(
            RuntimeStatus.Prefix + "{\"type\":\"PHASE\",\"event\":\"FINGERPRINT\",\"at\":\"2026-08-21T01:02:03Z\",\"extension\":{\"x\":1,\"x\":2}}");
    }

    private static void UnknownRuntimePairIsRejected()
    {
        AssertRuntimeParserFails(RuntimeLine("UNKNOWN", "PULSE"));
        AssertRuntimeParserFails(RuntimeLine("HEARTBEAT", "UNKNOWN"));
    }

    private static void InvalidRuntimePairIsRejected()
    {
        AssertRuntimeParserFails(RuntimeLine("PHASE", "PULSE"));
    }

    private static void BadRuntimeTimestampIsRejected()
    {
        AssertRuntimeParserFails(RuntimeStatus.Prefix + "{\"type\":\"PHASE\",\"event\":\"FINGERPRINT\",\"at\":\"2026-08-21T01:02:03+00:00\"}");
        AssertRuntimeParserFails(RuntimeStatus.Prefix + "{\"type\":\"PHASE\",\"event\":\"FINGERPRINT\",\"at\":\"2026-08-21 01:02:03Z\"}");
        AssertRuntimeParserFails(RuntimeStatus.Prefix + "{\"type\":\"PHASE\",\"event\":\"FINGERPRINT\",\"at\":\"not-a-time\"}");
    }

    private static void TrailingRuntimeJsonIsRejected()
    {
        AssertRuntimeParserFails(RuntimeLine("PHASE", "FINGERPRINT") + " {}");
    }

    private static void OversizedRuntimeRecordIsRejected()
    {
        string line = RuntimeStatus.Prefix + "{\"type\":\"PHASE\",\"event\":\"FINGERPRINT\",\"at\":\"2026-08-21T01:02:03Z\",\"extension\":\"" + new string('x', 600) + "\"}";
        AssertRuntimeParserFails(line);
    }

    private static void HeartbeatAndActivityAreTrackedSeparately()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("START", "RUN_STARTED"), TimeSpan.FromSeconds(1));
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(5));
        AssertEqual(TimeSpan.FromSeconds(5), tracker.LastHeartbeat);
        AssertEqual(TimeSpan.FromSeconds(1), tracker.LastOperationalActivity);
        AssertEqual("Collector started", tracker.LatestOperationalActivity);
    }

    private static void QuietRuntimeStateIsResponsive()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("START", "RUN_STARTED"), TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(15));
        AssertEqual(GuiRuntimeState.QuietButResponsive, tracker.GetState(TimeSpan.FromSeconds(15)));
    }

    private static void MissingRuntimeSignalDegrades()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        AssertEqual(GuiRuntimeState.Starting, tracker.GetState(TimeSpan.FromSeconds(14)));
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(15)));
    }

    private static void MissingRuntimeSignalBecomesUncertain()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        AssertEqual(GuiRuntimeState.LivenessUncertain, tracker.GetState(TimeSpan.FromSeconds(30)));
    }

    private static void RuntimeSignalRecovers()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("START", "RUN_STARTED"), TimeSpan.Zero);
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(20)));
        tracker.ObserveMalformed();
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(20)));
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(21));
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.FromSeconds(21));
        AssertEqual(GuiRuntimeState.Active, tracker.GetState(TimeSpan.FromSeconds(21)));
    }

    private static void FreshHeartbeatAndActivityAreActive()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(5));
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.FromSeconds(6));
        AssertEqual(GuiRuntimeState.Active, tracker.GetState(TimeSpan.FromSeconds(7)));
    }

    private static void FreshHeartbeatWithQuietActivityIsResponsive()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(5));
        AssertEqual(GuiRuntimeState.QuietButResponsive, tracker.GetState(TimeSpan.FromSeconds(19)));
    }

    private static void ActivityCannotMaskStaleHeartbeat()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.Zero);
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.FromSeconds(16));
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(16)));
    }

    private static void ActivityCannotMaskUncertainHeartbeat()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.Zero);
        tracker.Observe(Runtime("ACTIVITY", "PROVIDER_STARTED"), TimeSpan.FromSeconds(31));
        AssertEqual(GuiRuntimeState.LivenessUncertain, tracker.GetState(TimeSpan.FromSeconds(31)));
    }

    private static void ActivityWithoutHeartbeatDegrades()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.FromSeconds(10));
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(15)));
    }

    private static void ActivityWithoutHeartbeatBecomesUncertain()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("ACTIVITY", "PROVIDER_STARTED"), TimeSpan.FromSeconds(29));
        AssertEqual(GuiRuntimeState.LivenessUncertain, tracker.GetState(TimeSpan.FromSeconds(30)));
    }

    private static void HeartbeatRecoveryWithActivityIsActive()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.Zero);
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.FromSeconds(16));
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(16)));
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(17));
        tracker.Observe(Runtime("ACTIVITY", "PROVIDER_FINISHED"), TimeSpan.FromSeconds(18));
        AssertEqual(GuiRuntimeState.Active, tracker.GetState(TimeSpan.FromSeconds(18)));
    }

    private static void HeartbeatRecoveryWithQuietActivityIsResponsive()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.Zero);
        tracker.Observe(Runtime("PHASE", "COLLECTION"), TimeSpan.Zero);
        AssertEqual(GuiRuntimeState.SignalDegraded, tracker.GetState(TimeSpan.FromSeconds(16)));
        tracker.Observe(Runtime("HEARTBEAT", "PULSE"), TimeSpan.FromSeconds(17));
        AssertEqual(GuiRuntimeState.QuietButResponsive, tracker.GetState(TimeSpan.FromSeconds(20)));
    }

    private static void ProcessLifecycleStatesDoNotRegress()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.ProcessExited();
        AssertEqual(GuiRuntimeState.ProcessExited, tracker.GetState(TimeSpan.Zero));
        tracker.VerifyingResult();
        tracker.ProcessExited();
        AssertEqual(GuiRuntimeState.VerifyingResult, tracker.GetState(TimeSpan.Zero));
        tracker.Finish();
        tracker.ProcessExited();
        tracker.VerifyingResult();
        AssertEqual(GuiRuntimeState.Finished, tracker.GetState(TimeSpan.Zero));
    }

    private static void RuntimeTerminalEventIsNotFinalAuthority()
    {
        var tracker = new RuntimeLivenessTracker();
        tracker.Start(TimeSpan.Zero);
        tracker.Observe(Runtime("TERMINAL", "RUN_RETURNED"), TimeSpan.FromSeconds(1));
        AssertEqual(GuiRuntimeState.Active, tracker.GetState(TimeSpan.FromSeconds(1)));
        AssertTrue(tracker.GetState(TimeSpan.FromSeconds(1)) != GuiRuntimeState.Finished);
    }

    private static void RuntimeStateLabelsPreserveFrozenNames()
    {
        AssertEqual("QUIET_BUT_RESPONSIVE", RuntimeLivenessTracker.FormatState(GuiRuntimeState.QuietButResponsive));
        AssertEqual("SIGNAL_DEGRADED", RuntimeLivenessTracker.FormatState(GuiRuntimeState.SignalDegraded));
        AssertEqual("LIVENESS_UNCERTAIN", RuntimeLivenessTracker.FormatState(GuiRuntimeState.LivenessUncertain));
        AssertEqual("PROCESS_EXITED", RuntimeLivenessTracker.FormatState(GuiRuntimeState.ProcessExited));
        AssertEqual("VERIFYING_RESULT", RuntimeLivenessTracker.FormatState(GuiRuntimeState.VerifyingResult));
    }

    private static void MalformedRuntimeWarningIsFixed()
    {
        AssertEqual("Runtime status signal was malformed and was ignored.", CliRunner.MalformedRuntimeWarning);
        AssertTrue(!CliRunner.MalformedRuntimeWarning.Contains("raw-secret", StringComparison.Ordinal));
    }

    private static void StderrRuntimeRoutingHidesMalformedRawInput()
    {
        const string secret = "RAW-SECRET-MUST-NOT-ECHO";
        string input = RuntimeLine("PHASE", "FINGERPRINT") + "\n" +
            RuntimeStatus.Prefix + "{not-json-" + secret + "}\n" +
            "ordinary bounded diagnostic\n";
        using var stream = new MemoryStream(Encoding.UTF8.GetBytes(input));
        using var reader = new StreamReader(stream, Encoding.UTF8);
        MethodInfo method = typeof(CliRunner).GetMethod("ReadStandardErrorAsync", BindingFlags.NonPublic | BindingFlags.Static)
            ?? throw new InvalidOperationException("Runtime stderr router was not found.");
        int valid = 0;
        int malformed = 0;
        var validCallback = new Action<RuntimeStatus>(_ =>
        {
            valid++;
            throw new InvalidOperationException("observer failure must be isolated");
        });
        var malformedCallback = new Action(() => malformed++);
        Task task = (Task)(method.Invoke(null, new object?[] { reader, validCallback, malformedCallback })
            ?? throw new InvalidOperationException("Runtime stderr router returned no task."));
        task.GetAwaiter().GetResult();
        object capture = task.GetType().GetProperty("Result")?.GetValue(task)
            ?? throw new InvalidOperationException("Runtime stderr router returned no capture.");
        string diagnostics = (string)(capture.GetType().GetProperty("Diagnostics")?.GetValue(capture)
            ?? throw new InvalidOperationException("Runtime stderr capture had no diagnostics."));
        AssertEqual(1, valid);
        AssertEqual(1, malformed);
        AssertEqual("ordinary bounded diagnostic", diagnostics);
        AssertTrue(!diagnostics.Contains(secret, StringComparison.Ordinal));

        AssertEqual(1, (int)(capture.GetType().GetProperty("RuntimeEventCount")?.GetValue(capture) ?? -1));
        AssertEqual(1, (int)(capture.GetType().GetProperty("MalformedRuntimeSignalCount")?.GetValue(capture) ?? -1));
        AssertEqual(1, (int)(capture.GetType().GetProperty("OrdinaryStderrLineCount")?.GetValue(capture) ?? -1));
    }

    private static void LiveLogEnforcesLineBoundAndMarker()
    {
        var buffer = new LiveLogBuffer();
        for (int index = 0; index <= LiveLogBuffer.MaxLines; index++)
        {
            buffer.Append($"line-{index}");
        }
        AssertEqual(LiveLogBuffer.MaxLines, buffer.Lines.Count);
        AssertTrue(buffer.Lines.Contains(LiveLogBuffer.TrimMarker));
        AssertTrue(!buffer.Lines.Contains("line-0"));
    }

    private static void LiveLogEnforcesByteBound()
    {
        var buffer = new LiveLogBuffer();
        for (int index = 0; index < 100; index++)
        {
            buffer.Append(new string('x', 4096));
        }
        AssertTrue(buffer.FormattedBytes <= LiveLogBuffer.MaxFormattedBytes);
        AssertTrue(buffer.Lines.Contains(LiveLogBuffer.TrimMarker));
    }

    private static void FollowTailPausesAndResumes()
    {
        var follow = new LiveLogFollowState();
        AssertTrue(follow.FollowTail);
        follow.OnMessageAppended();
        AssertTrue(!follow.HasNewMessages);
        follow.Pause();
        follow.OnMessageAppended();
        AssertTrue(!follow.FollowTail && follow.HasNewMessages);
        follow.Resume();
        AssertTrue(follow.FollowTail && !follow.HasNewMessages);
    }

    private static RuntimeStatus Runtime(string type, string eventName)
    {
        return new RuntimeStatus(type, eventName, DateTimeOffset.Parse("2026-08-21T01:02:03Z"));
    }

    private static string RuntimeLine(string type, string eventName)
    {
        return RuntimeStatus.Prefix + $"{{\"type\":\"{type}\",\"event\":\"{eventName}\",\"at\":\"2026-08-21T01:02:03Z\"}}";
    }

    private static void AssertRuntimeParserFails(string line)
    {
        try
        {
            _ = RuntimeStatusParser.Parse(line);
            throw new InvalidOperationException("Expected runtime status parsing to fail.");
        }
        catch (RuntimeStatusException)
        {
        }
    }

    private static CliProcessResult ProcessResult(int exitCode, string stdout)
    {
        return new CliProcessResult(true, exitCode, stdout, string.Empty);
    }

    private static CliRunner CreateControlledRunner(CliProcessResult result)
    {
        Type delegateType = typeof(CliRunner).Assembly.GetType("Tannang.Gui.ChildProcessExecution")
            ?? throw new InvalidOperationException("Child process execution seam was not found.");
        MethodInfo method = typeof(TestRunner).GetMethod(nameof(ReturnControlledResult), BindingFlags.NonPublic | BindingFlags.Static)
            ?? throw new InvalidOperationException("Controlled execution method was not found.");
        Delegate execution = method.CreateDelegate(delegateType, result);
        ConstructorInfo constructor = typeof(CliRunner).GetConstructor(
                BindingFlags.Instance | BindingFlags.NonPublic,
                binder: null,
                new[] { delegateType },
                modifiers: null)
            ?? throw new InvalidOperationException("Controlled CliRunner constructor was not found.");
        return (CliRunner)constructor.Invoke(new object?[] { execution });
    }

    private static CliRunner CreateBaseDirectoryRunner(string baseDirectory)
    {
        ConstructorInfo constructor = typeof(CliRunner).GetConstructor(
                BindingFlags.Instance | BindingFlags.NonPublic,
                binder: null,
                new[] { typeof(string) },
                modifiers: null)
            ?? throw new InvalidOperationException("Base-directory CliRunner constructor was not found.");
        return (CliRunner)constructor.Invoke(new object?[] { baseDirectory });
    }

    private static CliRunner CreatePostStartFailureRunner(string baseDirectory)
    {
        Action<Process> afterStart = process =>
        {
            try
            {
                if (!process.HasExited)
                {
                    process.Kill(entireProcessTree: true);
                }
            }
            catch
            {
            }
            process.Dispose();
            throw new InvalidOperationException("controlled post-start failure");
        };
        ConstructorInfo constructor = typeof(CliRunner).GetConstructor(
                BindingFlags.Instance | BindingFlags.NonPublic,
                binder: null,
                new[] { typeof(string), typeof(Action<Process>) },
                modifiers: null)
            ?? throw new InvalidOperationException("Post-start failure CliRunner constructor was not found.");
        return (CliRunner)constructor.Invoke(new object?[] { baseDirectory, afterStart });
    }

    private static Task<CliProcessResult> ReturnControlledResult(
        CliProcessResult result,
        string output,
        string? caseId,
        Action<RuntimeStatus>? onRuntimeStatus,
        Action? onMalformedRuntimeStatus,
        Action? onProcessExited)
    {
        return Task.FromResult(result);
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
