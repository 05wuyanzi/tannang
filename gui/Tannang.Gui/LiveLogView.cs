// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Text;
using System.Windows.Forms;

namespace Tannang.Gui;

public sealed class LiveLogBuffer
{
    public const int MaxLines = 1000;
    public const int MaxFormattedBytes = 256 * 1024;
    private const int MaxSingleLineBytes = 4096;
    public const string TrimMarker = "[LOCAL] Earlier operational messages were removed from this view.";
    private static readonly int LineSeparatorBytes = Encoding.UTF8.GetByteCount(Environment.NewLine);

    private readonly List<string> _lines = new();
    private int _formattedBytes;
    private bool _trimMarkerAdded;

    public IReadOnlyList<string> Lines => _lines;
    public int FormattedBytes => _formattedBytes;

    public void Clear()
    {
        _lines.Clear();
        _formattedBytes = 0;
        _trimMarkerAdded = false;
    }

    public bool Append(string line)
    {
        string bounded = TruncateUtf8(line.Replace('\r', ' ').Replace('\n', ' '), MaxSingleLineBytes);
        int bytes = LineBytes(bounded);
        bool trimmed = false;
        while (_lines.Count > 0 && (_lines.Count >= MaxLines || _formattedBytes + bytes > MaxFormattedBytes))
        {
            RemoveOldestContentLine();
            trimmed = true;
        }
        if (trimmed && !_trimMarkerAdded)
        {
            string marker = TrimMarker;
            int markerBytes = LineBytes(marker);
            while (_lines.Count > 0 && (_lines.Count >= MaxLines - 1 || _formattedBytes + markerBytes + bytes > MaxFormattedBytes))
            {
                RemoveOldestContentLine();
            }
            _lines.Insert(0, marker);
            _formattedBytes += markerBytes;
            _trimMarkerAdded = true;
        }
        _lines.Add(bounded);
        _formattedBytes += bytes;
        return trimmed;
    }

    private void RemoveOldestContentLine()
    {
        int index = _trimMarkerAdded && _lines.Count > 1 && _lines[0] == TrimMarker ? 1 : 0;
        _formattedBytes -= LineBytes(_lines[index]);
        _lines.RemoveAt(index);
    }

    private static int LineBytes(string value) => Encoding.UTF8.GetByteCount(value) + LineSeparatorBytes;

    private static string TruncateUtf8(string value, int maxBytes)
    {
        if (Encoding.UTF8.GetByteCount(value) <= maxBytes)
        {
            return value;
        }
        int low = 0;
        int high = value.Length;
        while (low < high)
        {
            int middle = (low + high + 1) / 2;
            if (Encoding.UTF8.GetByteCount(value.AsSpan(0, middle)) <= maxBytes)
            {
                low = middle;
            }
            else
            {
                high = middle - 1;
            }
        }
        return value[..low];
    }
}

public sealed class LiveLogFollowState
{
    public bool FollowTail { get; private set; } = true;
    public bool HasNewMessages { get; private set; }

    public void Pause() => FollowTail = false;
    public void OnMessageAppended() => HasNewMessages = !FollowTail;
    public void Resume()
    {
        FollowTail = true;
        HasNewMessages = false;
    }
}

public sealed class LiveLogView : UserControl
{
    private readonly RichTextBox _log = new();
    private readonly LinkLabel _newMessages = new();
    private readonly LiveLogBuffer _buffer = new();
    private readonly LiveLogFollowState _follow = new();
    private bool _updatingLog;

    public LiveLogView()
    {
        Dock = DockStyle.Fill;
        _log.Dock = DockStyle.Fill;
        _log.ReadOnly = true;
        _log.WordWrap = false;
        _log.Font = new Font(FontFamily.GenericMonospace, 9F);
        _log.BackColor = SystemColors.Window;
        _log.AccessibleName = "Live operational log";
        _log.MouseWheel += (_, e) =>
        {
            if (e.Delta > 0)
            {
                _follow.Pause();
                UpdateIndicator();
            }
        };
        _log.VScroll += (_, _) =>
        {
            if (_updatingLog)
            {
                return;
            }
            if (IsAtBottom())
            {
                _follow.Resume();
            }
            else
            {
                _follow.Pause();
            }
            UpdateIndicator();
        };

        _newMessages.Text = "New messages";
        _newMessages.AutoSize = true;
        _newMessages.Visible = false;
        _newMessages.Dock = DockStyle.Bottom;
        _newMessages.TextAlign = ContentAlignment.MiddleRight;
        _newMessages.LinkClicked += (_, _) => ResumeFollowTail();

        Controls.Add(_log);
        Controls.Add(_newMessages);
    }

    public void ClearLog()
    {
        _buffer.Clear();
        _follow.Resume();
        _log.Clear();
        UpdateIndicator();
    }

    public void AppendRuntime(RuntimeStatus status)
    {
        string timestamp = status.At.UtcDateTime.ToString("HH:mm:ss'Z'");
        Append($"{timestamp}  {status.Type,-10}  {status.OperatorText}");
    }

    public void AppendLocalWarning(string message)
    {
        Append($"{DateTimeOffset.UtcNow:HH:mm:ss'Z'}  LOCAL       {message}");
    }

    private void Append(string line)
    {
        bool shouldFollow = _follow.FollowTail;
        bool trimmed = _buffer.Append(line);
        _follow.OnMessageAppended();
        _updatingLog = true;
        try
        {
            if (trimmed)
            {
                _log.Lines = _buffer.Lines.ToArray();
            }
            else
            {
                if (_log.TextLength > 0)
                {
                    _log.AppendText(Environment.NewLine);
                }
                _log.AppendText(_buffer.Lines[^1]);
            }
        }
        finally
        {
            _updatingLog = false;
        }
        if (shouldFollow)
        {
            _follow.Resume();
            ScrollToTail();
        }
        UpdateIndicator();
    }

    private void ResumeFollowTail()
    {
        _follow.Resume();
        ScrollToTail();
        UpdateIndicator();
    }

    private void ScrollToTail()
    {
        _log.SelectionStart = _log.TextLength;
        _log.SelectionLength = 0;
        _log.ScrollToCaret();
    }

    private bool IsAtBottom()
    {
        if (_log.TextLength == 0)
        {
            return true;
        }
        int visibleEnd = _log.GetCharIndexFromPosition(new Point(Math.Max(0, _log.ClientSize.Width - 4), Math.Max(0, _log.ClientSize.Height - 4)));
        return visibleEnd >= _log.TextLength - 2;
    }

    private void UpdateIndicator() => _newMessages.Visible = _follow.HasNewMessages;
}
