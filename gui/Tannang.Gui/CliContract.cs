// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

using System.Text.Json;

namespace Tannang.Gui;

public sealed class CliSummary
{
    public required string CollectionId { get; init; }
    public string? CaseId { get; init; }
    public required string RunState { get; init; }
    public string? OrchestrationReason { get; init; }
    public required bool FinalizationVerified { get; init; }
    public required string PackageReference { get; init; }
    public required IReadOnlyList<CliCapability> Capabilities { get; init; }
}

public sealed class CliCapability
{
    public required string Id { get; init; }
    public required bool Protected { get; init; }
    public required string Compatibility { get; init; }
    public required bool Attempted { get; init; }
    public required string ExecutionState { get; init; }
    public required string ExecutionReason { get; init; }
    public IReadOnlyList<string> MissingEvidence { get; init; } = Array.Empty<string>();
    public string? ReceiptReference { get; init; }
    public string? ArtifactReference { get; init; }
}

public sealed class CliContractException : Exception
{
    public CliContractException(string message) : base(message)
    {
    }
}

public static class CliContractParser
{
    public static CliSummary Parse(string stdout)
    {
        if (string.IsNullOrWhiteSpace(stdout))
        {
            throw new CliContractException("CLI stdout is empty.");
        }

        try
        {
            using JsonDocument document = JsonDocument.Parse(stdout);
            JsonElement root = document.RootElement;
            RequireKind(root, JsonValueKind.Object, "root");
            EnsureUniquePropertyNames(root);

            string collectionId = RequiredString(root, "collection_id", allowEmpty: false);
            string runState = RequiredString(root, "run_state", allowEmpty: false);
            RequireKnownRunState(runState);
            bool finalizationVerified = RequiredBoolean(root, "finalization_verified");
            string packageReference = RequiredString(root, "package_reference", allowEmpty: true);
            IReadOnlyList<CliCapability> capabilities = RequiredCapabilities(root);

            return new CliSummary
            {
                CollectionId = collectionId,
                CaseId = OptionalString(root, "case_id"),
                RunState = runState,
                OrchestrationReason = OptionalString(root, "orchestration_reason"),
                FinalizationVerified = finalizationVerified,
                PackageReference = packageReference,
                Capabilities = capabilities
            };
        }
        catch (CliContractException)
        {
            throw;
        }
        catch (JsonException exception)
        {
            throw new CliContractException($"CLI stdout is not one valid JSON document: {exception.Message}");
        }
    }

    private static IReadOnlyList<CliCapability> RequiredCapabilities(JsonElement root)
    {
        if (!root.TryGetProperty("capabilities", out JsonElement value) || value.ValueKind != JsonValueKind.Array)
        {
            throw new CliContractException("Required field 'capabilities' is missing or is not an array.");
        }

        var capabilities = new List<CliCapability>();
        foreach (JsonElement item in value.EnumerateArray())
        {
            RequireKind(item, JsonValueKind.Object, "capabilities item");
            JsonElement missingEvidenceValue = OptionalProperty(item, "missing_evidence");
            var missingEvidence = new List<string>();
            if (missingEvidenceValue.ValueKind != JsonValueKind.Undefined)
            {
                if (missingEvidenceValue.ValueKind != JsonValueKind.Array)
                {
                    throw new CliContractException("Capability field 'missing_evidence' is not an array.");
                }

                foreach (JsonElement evidence in missingEvidenceValue.EnumerateArray())
                {
                    if (evidence.ValueKind != JsonValueKind.String)
                    {
                        throw new CliContractException("Capability field 'missing_evidence' contains a non-string value.");
                    }

                    missingEvidence.Add(evidence.GetString()!);
                }
            }

            capabilities.Add(new CliCapability
            {
                Id = RequiredString(item, "id", allowEmpty: false),
                Protected = RequiredBoolean(item, "protected"),
                Compatibility = RequiredString(item, "compatibility", allowEmpty: false),
                Attempted = RequiredBoolean(item, "attempted"),
                ExecutionState = RequiredString(item, "execution_state", allowEmpty: false),
                ExecutionReason = RequiredString(item, "execution_reason", allowEmpty: false),
                MissingEvidence = missingEvidence,
                ReceiptReference = OptionalString(item, "receipt_reference"),
                ArtifactReference = OptionalString(item, "artifact_reference")
            });
        }

        return capabilities;
    }

    private static string RequiredString(JsonElement objectValue, string name, bool allowEmpty)
    {
        JsonElement value = RequiredProperty(objectValue, name);
        if (value.ValueKind != JsonValueKind.String)
        {
            throw new CliContractException($"Required field '{name}' is not a string.");
        }

        string result = value.GetString() ?? string.Empty;
        if (!allowEmpty && result.Length == 0)
        {
            throw new CliContractException($"Required field '{name}' is empty.");
        }

        return result;
    }

    private static bool RequiredBoolean(JsonElement objectValue, string name)
    {
        JsonElement value = RequiredProperty(objectValue, name);
        if (value.ValueKind != JsonValueKind.True && value.ValueKind != JsonValueKind.False)
        {
            throw new CliContractException($"Required field '{name}' is not a boolean.");
        }

        return value.GetBoolean();
    }

    private static string? OptionalString(JsonElement objectValue, string name)
    {
        JsonElement value = OptionalProperty(objectValue, name);
        if (value.ValueKind == JsonValueKind.Undefined || value.ValueKind == JsonValueKind.Null)
        {
            return null;
        }

        if (value.ValueKind != JsonValueKind.String)
        {
            throw new CliContractException($"Optional field '{name}' is not a string.");
        }

        return value.GetString();
    }

    private static JsonElement RequiredProperty(JsonElement objectValue, string name)
    {
        if (!objectValue.TryGetProperty(name, out JsonElement value))
        {
            throw new CliContractException($"Required field '{name}' is missing.");
        }

        return value;
    }

    private static JsonElement OptionalProperty(JsonElement objectValue, string name)
    {
        return objectValue.TryGetProperty(name, out JsonElement value) ? value : default;
    }

    private static void EnsureUniquePropertyNames(JsonElement value)
    {
        switch (value.ValueKind)
        {
            case JsonValueKind.Object:
            {
                var names = new HashSet<string>(StringComparer.Ordinal);
                foreach (JsonProperty property in value.EnumerateObject())
                {
                    if (!names.Add(property.Name))
                    {
                        throw new CliContractException($"JSON object contains duplicate property '{property.Name}'.");
                    }

                    EnsureUniquePropertyNames(property.Value);
                }

                break;
            }
            case JsonValueKind.Array:
                foreach (JsonElement item in value.EnumerateArray())
                {
                    EnsureUniquePropertyNames(item);
                }

                break;
        }
    }

    private static void RequireKnownRunState(string runState)
    {
        if (!string.Equals(runState, "COMPLETE", StringComparison.Ordinal) &&
            !string.Equals(runState, "PARTIAL", StringComparison.Ordinal) &&
            !string.Equals(runState, "FAILED", StringComparison.Ordinal))
        {
            throw new CliContractException("Required field 'run_state' is not a supported terminal state.");
        }
    }

    private static void RequireKind(JsonElement value, JsonValueKind expected, string label)
    {
        if (value.ValueKind != expected)
        {
            throw new CliContractException($"{label} is not a JSON {expected}.");
        }
    }
}
