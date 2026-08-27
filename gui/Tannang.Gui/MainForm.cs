// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Diagnostics;
using System.Globalization;
using System.Windows.Forms;

namespace Tannang.Gui;

public sealed class MainForm : Form
{
    private readonly TextBox _outputPath = new();
    private readonly Button _browseButton = new();
    private readonly TextBox _caseId = new();
    private readonly Button _startButton = new();
    private readonly Label _collectorStateLabel = new();
    private readonly Label _livenessLabel = new();
    private readonly Label _latestActivityLabel = new();
    private readonly Label _elapsedLabel = new();
    private readonly Label _summaryLabel = new();
    private readonly Label _diagnosticLabel = new();
    private readonly TextBox _packagePath = new();
    private readonly Button _copyDiagnosticsButton = new();
    private readonly LiveLogView _liveLog = new();
    private readonly CliRunner _cliRunner = new();
    private readonly RunStateGuard _runState = new();
    private readonly RuntimeLivenessTracker _liveness = new();
    private readonly Stopwatch _runClock = new();
    private readonly System.Windows.Forms.Timer _uiTimer = new() { Interval = 1000 };
    private CliExecutionDiagnostics? _lastDiagnostics;

    public MainForm()
    {
        Text = "Tannang Collection";
        ClientSize = new Size(920, 640);
        MinimumSize = new Size(820, 560);
        StartPosition = FormStartPosition.CenterScreen;
        AutoScaleMode = AutoScaleMode.Font;
        _uiTimer.Tick += (_, _) => UpdateRuntimeLabels();

        BuildControls();
        SetIdleState();
    }

    protected override void OnFormClosing(FormClosingEventArgs e)
    {
        if (_runState.IsRunning)
        {
            e.Cancel = true;
            MessageBox.Show(this, "Collection is still running. Please wait for it to finish.", "Collection in progress", MessageBoxButtons.OK, MessageBoxIcon.Information);
            return;
        }

        base.OnFormClosing(e);
    }

    private void BuildControls()
    {
        var layout = new TableLayoutPanel
        {
            Dock = DockStyle.Fill,
            Padding = new Padding(12),
            ColumnCount = 1,
            RowCount = 4,
            AutoSize = false
        };
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 92));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 70));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 144));

        layout.Controls.Add(BuildInputZone(), 0, 0);
        layout.Controls.Add(BuildStatusZone(), 0, 1);
        layout.Controls.Add(_liveLog, 0, 2);
        layout.Controls.Add(BuildTerminalZone(), 0, 3);
        Controls.Add(layout);
    }

    private Control BuildInputZone()
    {
        var input = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 3, RowCount = 2, Padding = new Padding(0, 0, 0, 8) };
        input.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 150));
        input.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        input.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 96));
        input.RowStyles.Add(new RowStyle(SizeType.Percent, 50));
        input.RowStyles.Add(new RowStyle(SizeType.Percent, 50));

        _outputPath.Dock = DockStyle.Fill;
        _outputPath.AccessibleName = "Evidence Package output path";
        _browseButton.Text = "Browse...";
        _browseButton.AutoSize = true;
        _browseButton.Click += BrowseButton_Click;
        AddRow(input, "Output destination", _outputPath, _browseButton, 0);

        _caseId.Dock = DockStyle.Fill;
        _caseId.AccessibleName = "Optional case ID";
        _startButton.Text = "Start";
        _startButton.AutoSize = true;
        _startButton.Click += StartButton_Click;
        AddRow(input, "Case ID (optional)", _caseId, _startButton, 1);
        return input;
    }

    private Control BuildStatusZone()
    {
        var status = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 4, RowCount = 2, Padding = new Padding(0, 0, 0, 8) };
        status.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 150));
        status.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 32));
        status.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 36));
        status.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 32));
        status.RowStyles.Add(new RowStyle(SizeType.Percent, 50));
        status.RowStyles.Add(new RowStyle(SizeType.Percent, 50));

        _collectorStateLabel.Dock = DockStyle.Fill;
        _collectorStateLabel.AccessibleName = "Collector process state";
        _livenessLabel.Dock = DockStyle.Fill;
        _livenessLabel.AccessibleName = "Collector liveness state";
        _latestActivityLabel.Dock = DockStyle.Fill;
        _latestActivityLabel.AutoEllipsis = true;
        _latestActivityLabel.AccessibleName = "Latest operational activity";
        _elapsedLabel.Dock = DockStyle.Fill;
        _elapsedLabel.AccessibleName = "Collection elapsed time";
        status.Controls.Add(new Label { Text = "Collector", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 0, 0);
        status.Controls.Add(_collectorStateLabel, 1, 0);
        status.Controls.Add(new Label { Text = "Liveness", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 2, 0);
        status.Controls.Add(_livenessLabel, 3, 0);
        status.Controls.Add(new Label { Text = "Latest activity", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 0, 1);
        status.Controls.Add(_latestActivityLabel, 1, 1);
        status.Controls.Add(new Label { Text = "Elapsed", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 2, 1);
        status.Controls.Add(_elapsedLabel, 3, 1);
        return status;
    }

    private Control BuildTerminalZone()
    {
        var terminal = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 3, RowCount = 3 };
        terminal.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 150));
        terminal.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        terminal.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 140));
        terminal.RowStyles.Add(new RowStyle(SizeType.Percent, 34));
        terminal.RowStyles.Add(new RowStyle(SizeType.Percent, 33));
        terminal.RowStyles.Add(new RowStyle(SizeType.Percent, 33));

        _summaryLabel.Dock = DockStyle.Fill;
        _summaryLabel.AutoEllipsis = true;
        _summaryLabel.AccessibleName = "Final result summary";
        terminal.Controls.Add(new Label { Text = "Final result", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 0, 0);
        terminal.Controls.Add(_summaryLabel, 1, 0);

        _copyDiagnosticsButton.Text = "Copy diagnostics";
        _copyDiagnosticsButton.AutoSize = true;
        _copyDiagnosticsButton.Visible = false;
        _copyDiagnosticsButton.Enabled = false;
        _copyDiagnosticsButton.AccessibleName = "Copy safe execution diagnostics";
        _copyDiagnosticsButton.Click += CopyDiagnosticsButton_Click;
        terminal.Controls.Add(_copyDiagnosticsButton, 2, 0);

        _diagnosticLabel.Dock = DockStyle.Fill;
        _diagnosticLabel.AutoEllipsis = true;
        _diagnosticLabel.AccessibleName = "Technical execution diagnostic";
        terminal.Controls.Add(new Label { Text = "Technical diagnostic", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 0, 1);
        terminal.Controls.Add(_diagnosticLabel, 1, 1);
        terminal.SetColumnSpan(_diagnosticLabel, 2);

        _packagePath.Dock = DockStyle.Fill;
        _packagePath.ReadOnly = true;
        _packagePath.TabStop = false;
        _packagePath.AccessibleName = "Verified Evidence Package location";
        terminal.Controls.Add(new Label { Text = "Evidence Package", Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, 0, 2);
        terminal.Controls.Add(_packagePath, 1, 2);
        terminal.SetColumnSpan(_packagePath, 2);
        return terminal;
    }

    private static void AddRow(TableLayoutPanel layout, string labelText, Control editor, Control action, int row)
    {
        layout.Controls.Add(new Label { Text = labelText, TextAlign = ContentAlignment.MiddleLeft, Dock = DockStyle.Fill }, 0, row);
        layout.Controls.Add(editor, 1, row);
        layout.Controls.Add(action, 2, row);
    }

    private void BrowseButton_Click(object? sender, EventArgs e)
    {
        using var dialog = new FolderBrowserDialog
        {
            Description = "Choose an existing parent directory for the Evidence Package.",
            UseDescriptionForTitle = true,
            ShowNewFolderButton = false
        };
        if (dialog.ShowDialog(this) != DialogResult.OK || string.IsNullOrEmpty(dialog.SelectedPath))
        {
            return;
        }

        string suffix = DateTimeOffset.UtcNow.ToString("yyyyMMdd-HHmmss", CultureInfo.InvariantCulture);
        _outputPath.Text = Path.Combine(dialog.SelectedPath, $"Tannang-{suffix}");
    }

    private async void StartButton_Click(object? sender, EventArgs e)
    {
        if (!_runState.TryEnter())
        {
            return;
        }

        string output = _outputPath.Text;
        string caseId = _caseId.Text;
        if (string.IsNullOrWhiteSpace(output))
        {
            _runState.Exit();
            MessageBox.Show(this, "Choose an output destination first.", "Output required", MessageBoxButtons.OK, MessageBoxIcon.Warning);
            return;
        }

        SetRunningState();
        try
        {
            CliProcessResult processResult = await _cliRunner.RunAsync(
                output,
                caseId,
                status => PostToUi(() => ObserveRuntime(status)),
                () => PostToUi(ObserveMalformedRuntime),
                () => PostToUi(ObserveProcessExited));
            _liveness.ProcessExited();
            UpdateRuntimeLabels();
            _liveness.VerifyingResult();
            UpdateRuntimeLabels();
            MappedResult result = ResultMapper.Map(processResult);
            _liveness.Finish();
            SetFinishedState(result);
        }
        catch (Exception)
        {
            _liveness.Finish();
            SetFinishedState(new MappedResult
            {
                State = GuiResultState.Failed,
                Summary = "Collection failed.",
                FailureDetail = "The GUI could not complete result mapping."
            });
        }
        finally
        {
            _uiTimer.Stop();
            _runClock.Stop();
            _runState.Exit();
            _outputPath.Enabled = true;
            _caseId.Enabled = true;
            _browseButton.Enabled = true;
            _startButton.Enabled = true;
        }
    }

    private void SetIdleState()
    {
        ClearDiagnostics();
        _collectorStateLabel.Text = "IDLE";
        _livenessLabel.Text = "STARTING";
        _latestActivityLabel.Text = "No activity yet.";
        _elapsedLabel.Text = "00:00:00";
        _summaryLabel.Text = "Ready to collect.";
        _packagePath.Text = string.Empty;
    }

    private void SetRunningState()
    {
        ClearDiagnostics();
        _liveness.Start();
        _liveLog.ClearLog();
        _runClock.Restart();
        _uiTimer.Start();
        _collectorStateLabel.Text = "RUNNING";
        _summaryLabel.Text = "Collection is running...";
        _packagePath.Text = string.Empty;
        _outputPath.Enabled = false;
        _caseId.Enabled = false;
        _browseButton.Enabled = false;
        _startButton.Enabled = false;
        UpdateRuntimeLabels();
    }

    private void ObserveRuntime(RuntimeStatus status)
    {
        if (!_runState.IsRunning)
        {
            return;
        }
        _liveness.Observe(status, _runClock.Elapsed);
        _liveLog.AppendRuntime(status);
        UpdateRuntimeLabels();
    }

    private void ObserveMalformedRuntime()
    {
        if (!_runState.IsRunning)
        {
            return;
        }
        _liveness.ObserveMalformed();
        _liveLog.AppendLocalWarning(CliRunner.MalformedRuntimeWarning);
        UpdateRuntimeLabels();
    }

    private void ObserveProcessExited()
    {
        if (!_runState.IsRunning)
        {
            return;
        }
        _liveness.ProcessExited();
        UpdateRuntimeLabels();
    }

    private void UpdateRuntimeLabels()
    {
        GuiRuntimeState state = _liveness.GetState(_runClock.Elapsed);
        _livenessLabel.Text = RuntimeLivenessTracker.FormatState(state);
        _elapsedLabel.Text = _runClock.Elapsed.ToString(@"hh\:mm\:ss");
        _latestActivityLabel.Text = _liveness.LatestOperationalActivity ?? "No activity yet.";
    }

    private void SetFinishedState(MappedResult result)
    {
        _collectorStateLabel.Text = $"FINISHED: {result.State.ToString().ToUpperInvariant()}";
        _summaryLabel.Text = result.Summary;
        _diagnosticLabel.Text = result.Diagnostics?.IsDiagnosticFailure == true
            ? result.Diagnostics.ToTechnicalSummary()
            : result.FailureDetail ?? string.Empty;
        _packagePath.Text = result.PackageReference ?? string.Empty;
        _lastDiagnostics = result.Diagnostics?.IsDiagnosticFailure == true ? result.Diagnostics : null;
        _copyDiagnosticsButton.Visible = _lastDiagnostics is not null;
        _copyDiagnosticsButton.Enabled = _lastDiagnostics is not null;
        UpdateRuntimeLabels();
    }

    private void ClearDiagnostics()
    {
        _lastDiagnostics = null;
        _diagnosticLabel.Text = string.Empty;
        _copyDiagnosticsButton.Visible = false;
        _copyDiagnosticsButton.Enabled = false;
    }

    private void CopyDiagnosticsButton_Click(object? sender, EventArgs e)
    {
        if (_lastDiagnostics is null)
        {
            return;
        }

        try
        {
            Clipboard.SetText(_lastDiagnostics.ToSafeClipboardText());
        }
        catch
        {
            MessageBox.Show(this, "Could not copy diagnostics.", "Copy diagnostics", MessageBoxButtons.OK, MessageBoxIcon.Warning);
        }
    }

    private void PostToUi(Action action)
    {
        if (IsDisposed || Disposing)
        {
            return;
        }
        try
        {
            BeginInvoke(action);
        }
        catch (InvalidOperationException)
        {
        }
    }
}
