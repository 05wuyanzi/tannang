// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Globalization;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Tannang.Gui;

public sealed class RuntimeStatusException : Exception
{
    public RuntimeStatusException(string message) : base(message)
    {
    }
}

public sealed record RuntimeStatus(string Type, string Event, DateTimeOffset At)
{
    public const string Prefix = "TANNANG_RUNTIME ";
    public const int MaxRecordBytes = 512;

    public bool IsHeartbeat => Type == "HEARTBEAT";
    public bool IsOperationalActivity => !IsHeartbeat;

    public string OperatorText => (Type, Event) switch
    {
        ("START", "RUN_STARTED") => "Collector started",
        ("PHASE", "FINGERPRINT") => "Preparing target fingerprint",
        ("PHASE", "RESOLUTION") => "Resolving protected baseline",
        ("PHASE", "COLLECTION") => "Collection phase entered",
        ("ACTIVITY", "PROVIDER_STARTED") => "Provider execution started",
        ("ACTIVITY", "PROVIDER_FINISHED") => "Provider execution returned",
        ("ACTIVITY", "ARTIFACT_SEALED") => "Artifact sealed",
        ("HEARTBEAT", "PULSE") => "Collector heartbeat received",
        ("FINALIZING", "STARTED") => "Finalizing Evidence Package",
        ("VERIFYING", "STARTED") => "Verifying final result",
        ("TERMINAL", "RUN_RETURNED") => "Collector returned control",
        _ => throw new RuntimeStatusException("Runtime status pair is not supported.")
    };
}

public static class RuntimeStatusParser
{
    public static RuntimeStatus Parse(string line)
    {
        if (string.IsNullOrEmpty(line) || !line.StartsWith(RuntimeStatus.Prefix, StringComparison.Ordinal))
        {
            throw new RuntimeStatusException("Runtime status prefix is missing.");
        }
        // ReadLineAsync removes the reporter's single LF terminator.
        if (Encoding.UTF8.GetByteCount(line) + 1 > RuntimeStatus.MaxRecordBytes)
        {
            throw new RuntimeStatusException("Runtime status record is too large.");
        }

        string payload = line[RuntimeStatus.Prefix.Length..];
        if (string.IsNullOrWhiteSpace(payload))
        {
            throw new RuntimeStatusException("Runtime status payload is empty.");
        }

        try
        {
            using JsonDocument document = JsonDocument.Parse(payload);
            JsonElement root = document.RootElement;
            if (root.ValueKind != JsonValueKind.Object)
            {
                throw new RuntimeStatusException("Runtime status payload is not an object.");
            }
            EnsureUniquePropertyNames(root);

            string type = RequiredString(root, "type");
            string eventName = RequiredString(root, "event");
            string atText = RequiredString(root, "at");
            ValidatePair(type, eventName);
            if (!Regex.IsMatch(atText, @"\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?Z\z", RegexOptions.CultureInvariant) ||
                !DateTimeOffset.TryParse(atText, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind, out DateTimeOffset at) ||
                at == default || at.Offset != TimeSpan.Zero)
            {
                throw new RuntimeStatusException("Runtime status timestamp is invalid.");
            }

            return new RuntimeStatus(type, eventName, at);
        }
        catch (RuntimeStatusException)
        {
            throw;
        }
        catch (JsonException exception)
        {
            throw new RuntimeStatusException($"Runtime status JSON is invalid: {exception.Message}");
        }
    }

    private static string RequiredString(JsonElement root, string name)
    {
        if (!root.TryGetProperty(name, out JsonElement value) || value.ValueKind != JsonValueKind.String)
        {
            throw new RuntimeStatusException($"Runtime status field '{name}' is missing or invalid.");
        }
        string result = value.GetString() ?? string.Empty;
        if (result.Length == 0)
        {
            throw new RuntimeStatusException($"Runtime status field '{name}' is empty.");
        }
        return result;
    }

    private static void ValidatePair(string type, string eventName)
    {
        bool valid = type switch
        {
            "START" => eventName == "RUN_STARTED",
            "PHASE" => eventName is "FINGERPRINT" or "RESOLUTION" or "COLLECTION",
            "ACTIVITY" => eventName is "PROVIDER_STARTED" or "PROVIDER_FINISHED" or "ARTIFACT_SEALED",
            "HEARTBEAT" => eventName == "PULSE",
            "FINALIZING" or "VERIFYING" => eventName == "STARTED",
            "TERMINAL" => eventName == "RUN_RETURNED",
            _ => false
        };
        if (!valid)
        {
            throw new RuntimeStatusException("Runtime status type/event pair is invalid.");
        }
    }

    private static void EnsureUniquePropertyNames(JsonElement value)
    {
        switch (value.ValueKind)
        {
            case JsonValueKind.Object:
                var names = new HashSet<string>(StringComparer.Ordinal);
                foreach (JsonProperty property in value.EnumerateObject())
                {
                    if (!names.Add(property.Name))
                    {
                        throw new RuntimeStatusException($"Runtime status contains duplicate property '{property.Name}'.");
                    }
                    EnsureUniquePropertyNames(property.Value);
                }
                break;
            case JsonValueKind.Array:
                foreach (JsonElement item in value.EnumerateArray())
                {
                    EnsureUniquePropertyNames(item);
                }
                break;
        }
    }
}
