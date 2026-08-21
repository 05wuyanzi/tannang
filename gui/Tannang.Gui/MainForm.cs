// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Globalization;
using System.Windows.Forms;

namespace Tannang.Gui;

public sealed class MainForm : Form
{
    private readonly TextBox _outputPath = new();
    private readonly Button _browseButton = new();
    private readonly TextBox _caseId = new();
    private readonly Button _startButton = new();
    private readonly Label _stateLabel = new();
    private readonly Label _summaryLabel = new();
    private readonly TextBox _packagePath = new();
    private readonly CliRunner _cliRunner = new();
    private readonly RunStateGuard _runState = new();

    public MainForm()
    {
        Text = "Tannang Collection";
        MinimumSize = new Size(620, 330);
        StartPosition = FormStartPosition.CenterScreen;
        AutoScaleMode = AutoScaleMode.Font;

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
            ColumnCount = 3,
            RowCount = 5,
            AutoSize = false
        };
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 150));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 96));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 42));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 42));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 48));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 58));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 58));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));

        _outputPath.Dock = DockStyle.Fill;
        _outputPath.AccessibleName = "Evidence Package output path";
        _browseButton.Text = "Browse...";
        _browseButton.AutoSize = true;
        _browseButton.Click += BrowseButton_Click;
        AddRow(layout, "Output destination", _outputPath, _browseButton, 0);

        _caseId.Dock = DockStyle.Fill;
        _caseId.AccessibleName = "Optional case ID";
        AddRow(layout, "Case ID (optional)", _caseId, new Panel(), 1);

        _startButton.Text = "Start";
        _startButton.AutoSize = true;
        _startButton.Click += StartButton_Click;
        _stateLabel.Dock = DockStyle.Fill;
        _stateLabel.TextAlign = ContentAlignment.MiddleLeft;
        _stateLabel.AccessibleName = "Current collection state";
        AddRow(layout, "State", _stateLabel, _startButton, 2);

        _summaryLabel.Dock = DockStyle.Fill;
        _summaryLabel.TextAlign = ContentAlignment.MiddleLeft;
        _summaryLabel.AutoEllipsis = true;
        _summaryLabel.AccessibleName = "Final result summary";
        layout.Controls.Add(new Label { Text = "Result", TextAlign = ContentAlignment.MiddleLeft, Dock = DockStyle.Fill }, 0, 3);
        layout.Controls.Add(_summaryLabel, 1, 3);
        layout.SetColumnSpan(_summaryLabel, 2);

        _packagePath.Dock = DockStyle.Fill;
        _packagePath.ReadOnly = true;
        _packagePath.TabStop = false;
        _packagePath.AccessibleName = "Verified Evidence Package location";
        layout.Controls.Add(new Label { Text = "Evidence Package", TextAlign = ContentAlignment.MiddleLeft, Dock = DockStyle.Fill }, 0, 4);
        layout.Controls.Add(_packagePath, 1, 4);
        layout.SetColumnSpan(_packagePath, 2);

        Controls.Add(layout);
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
            CliProcessResult processResult = await _cliRunner.RunAsync(output, caseId);
            MappedResult result = ResultMapper.Map(processResult);
            SetFinishedState(result);
        }
        catch (Exception exception)
        {
            SetFinishedState(new MappedResult
            {
                State = GuiResultState.Failed,
                Summary = "Collection failed.",
                FailureDetail = exception.Message
            });
        }
        finally
        {
            _runState.Exit();
            _outputPath.Enabled = true;
            _caseId.Enabled = true;
            _browseButton.Enabled = true;
            _startButton.Enabled = true;
        }
    }

    private void SetIdleState()
    {
        _stateLabel.Text = "IDLE";
        _summaryLabel.Text = "Ready to collect.";
        _packagePath.Text = string.Empty;
    }

    private void SetRunningState()
    {
        _stateLabel.Text = "RUNNING";
        _summaryLabel.Text = "Collection is running...";
        _packagePath.Text = string.Empty;
        _outputPath.Enabled = false;
        _caseId.Enabled = false;
        _browseButton.Enabled = false;
        _startButton.Enabled = false;
    }

    private void SetFinishedState(MappedResult result)
    {
        _stateLabel.Text = $"FINISHED: {result.State.ToString().ToUpperInvariant()}";
        _summaryLabel.Text = string.IsNullOrEmpty(result.FailureDetail)
            ? result.Summary
            : $"{result.Summary} {result.FailureDetail}";
        _packagePath.Text = result.PackageReference ?? string.Empty;
    }
}
