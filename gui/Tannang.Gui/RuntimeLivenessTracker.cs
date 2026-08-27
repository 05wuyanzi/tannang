// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

namespace Tannang.Gui;

public enum GuiRuntimeState
{
    Starting,
    Active,
    QuietButResponsive,
    SignalDegraded,
    LivenessUncertain,
    ProcessExited,
    VerifyingResult,
    Finished
}

public sealed class RuntimeLivenessTracker
{
    public static readonly TimeSpan HeartbeatCadence = TimeSpan.FromSeconds(5);
    public static readonly TimeSpan ActivityQuietThreshold = TimeSpan.FromSeconds(15);
    public static readonly TimeSpan SignalDegradedThreshold = TimeSpan.FromSeconds(15);
    public static readonly TimeSpan LivenessUncertainThreshold = TimeSpan.FromSeconds(30);

    private TimeSpan? _lastHeartbeat;
    private TimeSpan? _lastOperationalActivity;
    private bool _malformedSinceValidSignal;
    private GuiRuntimeState? _terminalState;
    private TimeSpan _startedAt;

    public TimeSpan? LastHeartbeat => _lastHeartbeat;
    public TimeSpan? LastOperationalActivity => _lastOperationalActivity;
    public string? LatestOperationalActivity { get; private set; }

    public void Start(TimeSpan? startedAt = null)
    {
        _startedAt = startedAt ?? TimeSpan.Zero;
        _lastHeartbeat = null;
        _lastOperationalActivity = null;
        _malformedSinceValidSignal = false;
        _terminalState = null;
        LatestOperationalActivity = null;
    }

    public void Observe(RuntimeStatus status, TimeSpan receivedAt)
    {
        _malformedSinceValidSignal = false;
        if (status.IsHeartbeat)
        {
            _lastHeartbeat = receivedAt;
        }
        else
        {
            _lastOperationalActivity = receivedAt;
            LatestOperationalActivity = status.OperatorText;
        }
    }

    public void ObserveMalformed()
    {
        _malformedSinceValidSignal = true;
    }

    public void ProcessExited()
    {
        if (_terminalState is not GuiRuntimeState.VerifyingResult and not GuiRuntimeState.Finished)
        {
            _terminalState = GuiRuntimeState.ProcessExited;
        }
    }

    public void VerifyingResult()
    {
        if (_terminalState != GuiRuntimeState.Finished)
        {
            _terminalState = GuiRuntimeState.VerifyingResult;
        }
    }

    public void Finish() => _terminalState = GuiRuntimeState.Finished;

    public static string FormatState(GuiRuntimeState state) => state switch
    {
        GuiRuntimeState.Starting => "STARTING",
        GuiRuntimeState.Active => "ACTIVE",
        GuiRuntimeState.QuietButResponsive => "QUIET_BUT_RESPONSIVE",
        GuiRuntimeState.SignalDegraded => "SIGNAL_DEGRADED",
        GuiRuntimeState.LivenessUncertain => "LIVENESS_UNCERTAIN",
        GuiRuntimeState.ProcessExited => "PROCESS_EXITED",
        GuiRuntimeState.VerifyingResult => "VERIFYING_RESULT",
        GuiRuntimeState.Finished => "FINISHED",
        _ => throw new ArgumentOutOfRangeException(nameof(state))
    };

    public GuiRuntimeState GetState(TimeSpan now)
    {
        if (_terminalState is not null)
        {
            return _terminalState.Value;
        }
        TimeSpan heartbeatAge = NonNegative(now - (_lastHeartbeat ?? _startedAt));
        if (heartbeatAge >= LivenessUncertainThreshold)
        {
            return GuiRuntimeState.LivenessUncertain;
        }
        if (_malformedSinceValidSignal || heartbeatAge >= SignalDegradedThreshold)
        {
            return GuiRuntimeState.SignalDegraded;
        }
        if (_lastOperationalActivity is not null &&
            NonNegative(now - _lastOperationalActivity.Value) < ActivityQuietThreshold)
        {
            return GuiRuntimeState.Active;
        }
        if (_lastHeartbeat is null)
        {
            return GuiRuntimeState.Starting;
        }
        return GuiRuntimeState.QuietButResponsive;
    }

    private static TimeSpan NonNegative(TimeSpan value) => value < TimeSpan.Zero ? TimeSpan.Zero : value;
}
