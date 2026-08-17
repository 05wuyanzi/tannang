# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$SourceRepository,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-f]{40}$')]
    [string]$SourceRevision,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-f]{40}$')]
    [string]$ExpectedSourceTree,

    [Parameter(Mandatory = $true)]
    [string]$AcceptanceRoot,

    [string]$CaseId = 'M5-WIN-AMD64-ACCEPT-001'
)

$ErrorActionPreference = 'Stop'
$acceptanceContract = 'TANNANG_M5_WINDOWS_AMD64_ACCEPTANCE_V1'
$expectedCapability = 'PROCESS_IDENTITY_SNAPSHOT'
$expectedArtifactClass = 'HEADLESS_PORTABLE_WINDOWS_AMD64'
$expectedPortableFiles = @('BUILD-INFO.json', 'LICENSE', 'SHA256SUMS.txt', 'tannang.exe')
$utf8WithoutBOM = New-Object Text.UTF8Encoding($false)

function Assert-OrdinaryWindowsDriveAbsolutePath {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Label
    )

    # Classify the namespace before any provider or filesystem access. The v1
    # acceptance contract intentionally excludes UNC, device, volume-GUID,
    # drive-relative, and other ambiguous namespaces.
    if ([string]::IsNullOrWhiteSpace($Path) -or $Path -cnotmatch '^[A-Za-z]:[\\/]') {
        throw "$Label must be an ordinary Windows drive-rooted absolute path."
    }
}

function Assert-AcceptanceDriveTypeAllowed {
    param(
        [Parameter(Mandatory = $true)][IO.DriveType]$DriveType,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($DriveType -ne [IO.DriveType]::Fixed -and $DriveType -ne [IO.DriveType]::Removable) {
        throw "$Label must be on a local fixed or removable volume; observed drive type $DriveType."
    }
}

function Get-AcceptanceDriveType {
    param([Parameter(Mandatory = $true)][string]$CanonicalPath)

    $driveRoot = [IO.Path]::GetPathRoot($CanonicalPath)
    if ([string]::IsNullOrWhiteSpace($driveRoot)) {
        throw "Validated path has no drive root."
    }
    return ([IO.DriveInfo]::new($driveRoot)).DriveType
}

function Get-CanonicalPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    $fullPath = [IO.Path]::GetFullPath($Path)
    $root = [IO.Path]::GetPathRoot($fullPath)
    if ([string]::Equals($fullPath, $root, [StringComparison]::OrdinalIgnoreCase)) {
        return $root
    }
    return $fullPath.TrimEnd([char[]]@([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar))
}

function Test-PathWithin {
    param(
        [Parameter(Mandatory = $true)][string]$Child,
        [Parameter(Mandatory = $true)][string]$Parent
    )

    $canonicalChild = Get-CanonicalPath -Path $Child
    $canonicalParent = Get-CanonicalPath -Path $Parent
    $prefix = $canonicalParent
    if (-not $prefix.EndsWith([string][IO.Path]::DirectorySeparatorChar, [StringComparison]::Ordinal)) {
        $prefix += [IO.Path]::DirectorySeparatorChar
    }
    return $canonicalChild.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
}

function Assert-NoReparseAncestors {
    param([Parameter(Mandatory = $true)][string]$Path)

    $item = Get-Item -LiteralPath $Path -Force
    while ($null -ne $item) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Path traverses a reparse point."
        }
        $item = $item.Parent
    }
}

function Assert-NoReparseTree {
    param([Parameter(Mandatory = $true)][string]$Path)

    $rootItem = Get-Item -LiteralPath $Path -Force
    if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Validated tree root is a reparse point."
    }
    foreach ($item in @(Get-ChildItem -LiteralPath $Path -Recurse -Force)) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Validated tree contains a reparse point."
        }
    }
}

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value
    )

    [IO.File]::WriteAllText($Path, $Value, $utf8WithoutBOM)
}

function Get-Sha256 {
    param([Parameter(Mandatory = $true)][string]$Path)

    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Invoke-GitCapture {
    param(
        [Parameter(Mandatory = $true)][string]$Repository,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )

    $output = @(& git -C $Repository @Arguments 2>&1)
    $exitCode = $LASTEXITCODE
    $text = ($output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
    if ($exitCode -ne 0) {
        throw "git $($Arguments -join ' ') failed: $text"
    }
    return $text.Trim()
}

function New-StrictJsonNode {
    param(
        [Parameter(Mandatory = $true)][string]$Kind,
        [AllowNull()]$Value,
        [AllowNull()][string]$Raw
    )

    return [pscustomobject]@{ Kind = $Kind; Value = $Value; Raw = $Raw }
}

function Skip-StrictJsonWhitespace {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][ref]$Index
    )

    while ($Index.Value -lt $Text.Length) {
        $character = $Text[$Index.Value]
        if ($character -ne ' ' -and $character -ne "`t" -and $character -ne "`r" -and $character -ne "`n") {
            break
        }
        $Index.Value++
    }
}

function ConvertFrom-StrictJsonStringToken {
    param([Parameter(Mandatory = $true)][string]$Raw)

    $builder = New-Object Text.StringBuilder
    for ($index = 1; $index -lt ($Raw.Length - 1); $index++) {
        $character = $Raw[$index]
        if ($character -ne '\') {
            $null = $builder.Append($character)
            continue
        }
        $index++
        $escaped = $Raw[$index]
        switch ($escaped) {
            '"' { $null = $builder.Append('"') }
            '\' { $null = $builder.Append('\') }
            '/' { $null = $builder.Append('/') }
            'b' { $null = $builder.Append([char]0x08) }
            'f' { $null = $builder.Append([char]0x0c) }
            'n' { $null = $builder.Append("`n") }
            'r' { $null = $builder.Append("`r") }
            't' { $null = $builder.Append("`t") }
            'u' {
                $hex = $Raw.Substring($index + 1, 4)
                $null = $builder.Append([char][Convert]::ToInt32($hex, 16))
                $index += 4
            }
            default { throw "Strict JSON string contains an unsupported escape." }
        }
    }
    return $builder.ToString()
}

function Read-StrictJsonStringNode {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][ref]$Index
    )

    if ($Index.Value -ge $Text.Length -or $Text[$Index.Value] -ne '"') {
        throw "Strict JSON expected a string."
    }
    $start = $Index.Value
    $Index.Value++
    while ($Index.Value -lt $Text.Length) {
        $character = $Text[$Index.Value]
        if ($character -eq '"') {
            $Index.Value++
            $raw = $Text.Substring($start, $Index.Value - $start)
            $value = ConvertFrom-StrictJsonStringToken -Raw $raw
            return New-StrictJsonNode -Kind 'String' -Value $value -Raw $raw
        }
        if ([int]$character -lt 0x20) {
            throw "Strict JSON string contains an unescaped control character."
        }
        if ($character -eq '\') {
            $Index.Value++
            if ($Index.Value -ge $Text.Length) {
                throw "Strict JSON string ends in an incomplete escape."
            }
            $escaped = $Text[$Index.Value]
            if ($escaped -eq 'u') {
                if (($Index.Value + 4) -ge $Text.Length) {
                    throw "Strict JSON string contains an incomplete Unicode escape."
                }
                for ($offset = 1; $offset -le 4; $offset++) {
                    if ($Text[$Index.Value + $offset] -cnotmatch '[0-9a-fA-F]') {
                        throw "Strict JSON string contains an invalid Unicode escape."
                    }
                }
                $Index.Value += 4
            }
            elseif ('"\/bfnrt'.IndexOf($escaped) -lt 0) {
                throw "Strict JSON string contains an unsupported escape."
            }
        }
        $Index.Value++
    }
    throw "Strict JSON string is unterminated."
}

function Read-StrictJsonNumberNode {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][ref]$Index
    )

    $match = [regex]::Match(
        $Text.Substring($Index.Value),
        '^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?',
        [Text.RegularExpressions.RegexOptions]::CultureInvariant
    )
    if (-not $match.Success) {
        throw "Strict JSON contains an invalid number."
    }
    $Index.Value += $match.Length
    return New-StrictJsonNode -Kind 'Number' -Value $match.Value -Raw $match.Value
}

function Read-StrictJsonArrayNode {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][ref]$Index
    )

    $items = New-Object Collections.ArrayList
    $Index.Value++
    Skip-StrictJsonWhitespace -Text $Text -Index $Index
    if ($Index.Value -lt $Text.Length -and $Text[$Index.Value] -eq ']') {
        $Index.Value++
        return New-StrictJsonNode -Kind 'Array' -Value $items -Raw $null
    }
    while ($true) {
        $item = Read-StrictJsonValueNode -Text $Text -Index $Index
        $null = $items.Add($item)
        Skip-StrictJsonWhitespace -Text $Text -Index $Index
        if ($Index.Value -ge $Text.Length) {
            throw "Strict JSON array is unterminated."
        }
        if ($Text[$Index.Value] -eq ']') {
            $Index.Value++
            return New-StrictJsonNode -Kind 'Array' -Value $items -Raw $null
        }
        if ($Text[$Index.Value] -ne ',') {
            throw "Strict JSON array expected a comma."
        }
        $Index.Value++
        Skip-StrictJsonWhitespace -Text $Text -Index $Index
    }
}

function Read-StrictJsonObjectNode {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][ref]$Index
    )

    $properties = [Collections.Generic.Dictionary[string, object]]::new([StringComparer]::Ordinal)
    $Index.Value++
    Skip-StrictJsonWhitespace -Text $Text -Index $Index
    if ($Index.Value -lt $Text.Length -and $Text[$Index.Value] -eq '}') {
        $Index.Value++
        return New-StrictJsonNode -Kind 'Object' -Value $properties -Raw $null
    }
    while ($true) {
        $nameNode = Read-StrictJsonStringNode -Text $Text -Index $Index
        $name = [string]$nameNode.Value
        if ($properties.ContainsKey($name)) {
            throw "Strict JSON contains duplicate object property $name."
        }
        Skip-StrictJsonWhitespace -Text $Text -Index $Index
        if ($Index.Value -ge $Text.Length -or $Text[$Index.Value] -ne ':') {
            throw "Strict JSON object expected a colon."
        }
        $Index.Value++
        $properties.Add($name, (Read-StrictJsonValueNode -Text $Text -Index $Index))
        Skip-StrictJsonWhitespace -Text $Text -Index $Index
        if ($Index.Value -ge $Text.Length) {
            throw "Strict JSON object is unterminated."
        }
        if ($Text[$Index.Value] -eq '}') {
            $Index.Value++
            return New-StrictJsonNode -Kind 'Object' -Value $properties -Raw $null
        }
        if ($Text[$Index.Value] -ne ',') {
            throw "Strict JSON object expected a comma."
        }
        $Index.Value++
        Skip-StrictJsonWhitespace -Text $Text -Index $Index
    }
}

function Read-StrictJsonValueNode {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][ref]$Index
    )

    Skip-StrictJsonWhitespace -Text $Text -Index $Index
    if ($Index.Value -ge $Text.Length) {
        throw "Strict JSON ended before a value."
    }
    switch ($Text[$Index.Value]) {
        '{' { return Read-StrictJsonObjectNode -Text $Text -Index $Index }
        '[' { return Read-StrictJsonArrayNode -Text $Text -Index $Index }
        '"' { return Read-StrictJsonStringNode -Text $Text -Index $Index }
        't' {
            if (($Index.Value + 4) -le $Text.Length -and $Text.Substring($Index.Value, 4) -ceq 'true') {
                $Index.Value += 4
                return New-StrictJsonNode -Kind 'Boolean' -Value $true -Raw 'true'
            }
        }
        'f' {
            if (($Index.Value + 5) -le $Text.Length -and $Text.Substring($Index.Value, 5) -ceq 'false') {
                $Index.Value += 5
                return New-StrictJsonNode -Kind 'Boolean' -Value $false -Raw 'false'
            }
        }
        'n' {
            if (($Index.Value + 4) -le $Text.Length -and $Text.Substring($Index.Value, 4) -ceq 'null') {
                $Index.Value += 4
                return New-StrictJsonNode -Kind 'Null' -Value $null -Raw 'null'
            }
        }
        default {
            if ($Text[$Index.Value] -eq '-' -or [char]::IsDigit($Text[$Index.Value])) {
                return Read-StrictJsonNumberNode -Text $Text -Index $Index
            }
        }
    }
    throw "Strict JSON contains an invalid value token."
}

function ConvertFrom-StrictJsonText {
    param(
        [Parameter(Mandatory = $true)][AllowEmptyString()][string]$Text,
        [Parameter(Mandatory = $true)][string]$Label
    )

    try {
        $index = 0
        $node = Read-StrictJsonValueNode -Text $Text -Index ([ref]$index)
        Skip-StrictJsonWhitespace -Text $Text -Index ([ref]$index)
        if ($index -ne $Text.Length) {
            throw "Strict JSON contains trailing data."
        }
        return $node
    }
    catch {
        throw "$Label is not strict JSON: $($_.Exception.Message)"
    }
}

function Read-StrictJsonNode {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Label
    )

    return ConvertFrom-StrictJsonText -Text ([IO.File]::ReadAllText($Path)) -Label $Label
}

function Read-JsonObject {
    param([Parameter(Mandatory = $true)][string]$Path)

    $text = [IO.File]::ReadAllText($Path)
    $label = [IO.Path]::GetFileName($Path)
    $node = ConvertFrom-StrictJsonText -Text $text -Label $label
    if ($node.Kind -cne 'Object') {
        throw "Expected one JSON object in $label."
    }
    try {
        return ConvertFrom-Json -InputObject $text -ErrorAction Stop
    }
    catch {
        throw "Invalid JSON in $label."
    }
}

function ConvertFrom-JsonObjectText {
    param(
        [Parameter(Mandatory = $true)][string]$Value,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $node = ConvertFrom-StrictJsonText -Text $Value -Label $Label
    if ($node.Kind -cne 'Object') {
        throw "$Label did not return one JSON object."
    }
    try {
        return ConvertFrom-Json -InputObject $Value -ErrorAction Stop
    }
    catch {
        throw "$Label did not return one valid JSON object."
    }
}

function Assert-StrictJsonObjectShape {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string[]]$Required,
        [Parameter(Mandatory = $true)][string[]]$Allowed,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Node.Kind -cne 'Object') {
        throw "$Label must be a JSON object."
    }
    foreach ($name in $Node.Value.Keys) {
        if ($Allowed -cnotcontains $name) {
            throw "$Label contains unknown property $name."
        }
    }
    foreach ($name in $Required) {
        if (-not $Node.Value.ContainsKey($name)) {
            throw "$Label is missing property $name."
        }
    }
}

function Get-StrictJsonProperty {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Node.Kind -cne 'Object' -or -not $Node.Value.ContainsKey($Name)) {
        throw "$Label is missing property $Name."
    }
    return $Node.Value[$Name]
}

function Test-StrictJsonProperty {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Name
    )

    return $Node.Kind -ceq 'Object' -and $Node.Value.ContainsKey($Name)
}

function Get-StrictJsonString {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label,
        [switch]$AllowEmpty
    )

    if ($Node.Kind -cne 'String' -or (-not $AllowEmpty -and [string]::IsNullOrWhiteSpace([string]$Node.Value))) {
        throw "$Label must be a non-empty JSON string."
    }
    return [string]$Node.Value
}

function Get-StrictJsonBoolean {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Node.Kind -cne 'Boolean') {
        throw "$Label must be a JSON boolean."
    }
    return [bool]$Node.Value
}

function Get-StrictJsonInteger {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][Int64]$Minimum,
        [Parameter(Mandatory = $true)][Int64]$Maximum,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Node.Kind -cne 'Number' -or [string]$Node.Raw -cnotmatch '^-?(?:0|[1-9][0-9]*)$') {
        throw "$Label must be a native JSON integer."
    }
    $value = [Int64]0
    if (-not [Int64]::TryParse(
        [string]$Node.Raw,
        [Globalization.NumberStyles]::AllowLeadingSign,
        [Globalization.CultureInfo]::InvariantCulture,
        [ref]$value
    ) -or $value -lt $Minimum -or $value -gt $Maximum) {
        throw "$Label is outside its committed integer range."
    }
    return $value
}

function Assert-StrictJsonTimestamp {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $value = Get-StrictJsonString -Node $Node -Label $Label
    if ($value -cnotmatch '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$') {
        throw "$Label is not RFC3339Nano syntax."
    }
    $parsed = [DateTimeOffset]::MinValue
    if (-not [DateTimeOffset]::TryParse(
        $value,
        [Globalization.CultureInfo]::InvariantCulture,
        [Globalization.DateTimeStyles]::RoundtripKind,
        [ref]$parsed
    )) {
        throw "$Label is not a valid timestamp."
    }
}

function Assert-ExactPropertySet {
    param(
        [Parameter(Mandatory = $true)]$Value,
        [Parameter(Mandatory = $true)][string[]]$Expected,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $actual = @($Value.PSObject.Properties.Name)
    if ($actual.Count -ne $Expected.Count) {
        throw "$Label has an unexpected property count."
    }
    foreach ($name in $Expected) {
        if ($actual -cnotcontains $name) {
            throw "$Label is missing property $name."
        }
    }
}

function Assert-AllowedPropertySet {
    param(
        [Parameter(Mandatory = $true)]$Value,
        [Parameter(Mandatory = $true)][string[]]$Required,
        [Parameter(Mandatory = $true)][string[]]$Allowed,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $actual = @($Value.PSObject.Properties.Name)
    foreach ($name in $actual) {
        if ($Allowed -cnotcontains $name) {
            throw "$Label contains unexpected property $name."
        }
    }
    foreach ($name in $Required) {
        if ($actual -cnotcontains $name) {
            throw "$Label is missing property $name."
        }
    }
}

function Get-StrictJsonUInt64 {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][UInt64]$Minimum,
        [Parameter(Mandatory = $true)][UInt64]$Maximum,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Node.Kind -cne 'Number' -or [string]$Node.Raw -cnotmatch '^(?:0|[1-9][0-9]*)$') {
        throw "$Label must be a non-negative native JSON integer."
    }
    $value = [UInt64]0
    if (-not [UInt64]::TryParse(
        [string]$Node.Raw,
        [Globalization.NumberStyles]::None,
        [Globalization.CultureInfo]::InvariantCulture,
        [ref]$value
    ) -or $value -lt $Minimum -or $value -gt $Maximum) {
        throw "$Label is outside its committed integer range."
    }
    return $value
}

function Get-StrictJsonEnum {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string[]]$Allowed,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $value = Get-StrictJsonString -Node $Node -Label $Label
    if ($Allowed -cnotcontains $value) {
        throw "$Label contains an unsupported value."
    }
    return $value
}

function Assert-StrictJsonStringLength {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][int]$Minimum,
        [Parameter(Mandatory = $true)][int]$Maximum,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Node.Kind -cne 'String' -or ([string]$Node.Value).Length -lt $Minimum -or ([string]$Node.Value).Length -gt $Maximum) {
        throw "$Label violates its committed string length."
    }
}

function Test-StrictJsonNodeEqual {
    param(
        [Parameter(Mandatory = $true)]$Left,
        [Parameter(Mandatory = $true)]$Right
    )

    if ($Left.Kind -cne $Right.Kind) {
        return $false
    }
    switch ($Left.Kind) {
        'Object' {
            if ($Left.Value.Count -ne $Right.Value.Count) {
                return $false
            }
            foreach ($name in $Left.Value.Keys) {
                if (-not $Right.Value.ContainsKey($name) -or -not (Test-StrictJsonNodeEqual -Left $Left.Value[$name] -Right $Right.Value[$name])) {
                    return $false
                }
            }
            return $true
        }
        'Array' {
            if ($Left.Value.Count -ne $Right.Value.Count) {
                return $false
            }
            for ($index = 0; $index -lt $Left.Value.Count; $index++) {
                if (-not (Test-StrictJsonNodeEqual -Left $Left.Value[$index] -Right $Right.Value[$index])) {
                    return $false
                }
            }
            return $true
        }
        'Number' { return [string]$Left.Raw -ceq [string]$Right.Raw }
        'Null' { return $true }
        default { return $Left.Value -ceq $Right.Value }
    }
}

function Assert-CollectionID {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $value = Get-StrictJsonString -Node $Node -Label $Label
    if ($value -cnotmatch '^COL-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') {
        throw "$Label must be a lowercase canonical UUIDv4 with the COL- prefix."
    }
    return $value
}

function Assert-ProbeFieldSchema {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][ValidateSet('String', 'Boolean', 'UInt32', 'UInt64', 'BasisPoints')][string]$ValueType,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $required = @('state', 'source', 'captured_at')
    $allowed = @('state', 'value', 'source', 'captured_at', 'error_reason', 'error_code')
    Assert-StrictJsonObjectShape -Node $Node -Required $required -Allowed $allowed -Label $Label
    $state = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $Node -Name 'state' -Label $Label) `
        -Allowed @('KNOWN', 'UNAVAILABLE', 'FAILED', 'UNSUPPORTED') -Label "$Label.state"
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'source' -Label $Label) -Label "$Label.source"
    Assert-StrictJsonTimestamp -Node (Get-StrictJsonProperty -Node $Node -Name 'captured_at' -Label $Label) -Label "$Label.captured_at"
    $hasValue = Test-StrictJsonProperty -Node $Node -Name 'value'
    $hasReason = Test-StrictJsonProperty -Node $Node -Name 'error_reason'
    $hasCode = Test-StrictJsonProperty -Node $Node -Name 'error_code'
    $value = $null
    if ($state -ceq 'KNOWN') {
        if (-not $hasValue -or $hasReason -or $hasCode) {
            throw "$Label KNOWN state requires value and forbids error fields."
        }
        $valueNode = Get-StrictJsonProperty -Node $Node -Name 'value' -Label $Label
        switch ($ValueType) {
            'String' { $value = Get-StrictJsonString -Node $valueNode -Label "$Label.value" }
            'Boolean' { $value = Get-StrictJsonBoolean -Node $valueNode -Label "$Label.value" }
            'UInt32' { $value = Get-StrictJsonUInt64 -Node $valueNode -Minimum 0 -Maximum ([UInt32]::MaxValue) -Label "$Label.value" }
            'UInt64' { $value = Get-StrictJsonUInt64 -Node $valueNode -Minimum 0 -Maximum ([UInt64]::MaxValue) -Label "$Label.value" }
            'BasisPoints' { $value = Get-StrictJsonUInt64 -Node $valueNode -Minimum 0 -Maximum 10000 -Label "$Label.value" }
        }
    }
    else {
        if ($hasValue -or -not $hasReason) {
            throw "$Label non-KNOWN state requires error_reason and forbids value."
        }
        $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'error_reason' -Label $Label) -Label "$Label.error_reason"
        if ($hasCode) {
            $null = Get-StrictJsonUInt64 -Node (Get-StrictJsonProperty -Node $Node -Name 'error_code' -Label $Label) `
                -Minimum 1 -Maximum ([UInt32]::MaxValue) -Label "$Label.error_code"
        }
    }
    return [pscustomobject]@{ State = $state; Value = $value }
}

function Assert-TargetFingerprintSchema {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $allowed = @('platform', 'os_family', 'version', 'build', 'architecture', 'privilege', 'elevated', 'runtime_lane', 'probe')
    Assert-StrictJsonObjectShape -Node $Node -Required @('platform') -Allowed $allowed -Label $Label
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'platform' -Label $Label) -Label "$Label.platform"
    $hasProbe = Test-StrictJsonProperty -Node $Node -Name 'probe'
    foreach ($name in @('os_family', 'version', 'build', 'architecture', 'privilege', 'runtime_lane')) {
        if (Test-StrictJsonProperty -Node $Node -Name $name) {
            $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name $name -Label $Label) -Label "$Label.$name"
        }
    }
    if (Test-StrictJsonProperty -Node $Node -Name 'elevated') {
        $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $Node -Name 'elevated' -Label $Label) -Label "$Label.elevated"
    }
    if (-not $hasProbe) {
        foreach ($name in @('os_family', 'version', 'build', 'architecture', 'privilege', 'elevated', 'runtime_lane')) {
            if (-not (Test-StrictJsonProperty -Node $Node -Name $name)) {
                throw "$Label synthetic fingerprint is missing $name."
            }
        }
        return
    }
    foreach ($name in @('os_family', 'version', 'build', 'architecture')) {
        if (-not (Test-StrictJsonProperty -Node $Node -Name $name)) {
            throw "$Label real fingerprint is missing compatibility mirror $name."
        }
    }

    $probe = Get-StrictJsonProperty -Node $Node -Name 'probe' -Label $Label
    $probeRequired = @(
        'os_version', 'os_build', 'native_architecture', 'process_architecture',
        'logical_processor_count', 'total_physical_memory_bytes', 'available_physical_memory_bytes',
        'elevated', 'token_elevation_type', 'output_volume'
    )
    Assert-StrictJsonObjectShape -Node $probe -Required $probeRequired -Allowed ($probeRequired + @('cpu_busy_basis_points')) -Label "$Label.probe"
    $osVersion = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'os_version' -Label "$Label.probe") -ValueType String -Label "$Label.probe.os_version"
    $osBuild = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'os_build' -Label "$Label.probe") -ValueType String -Label "$Label.probe.os_build"
    $nativeArchitecture = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'native_architecture' -Label "$Label.probe") -ValueType String -Label "$Label.probe.native_architecture"
    $processArchitecture = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'process_architecture' -Label "$Label.probe") -ValueType String -Label "$Label.probe.process_architecture"
    $logicalProcessors = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'logical_processor_count' -Label "$Label.probe") -ValueType UInt32 -Label "$Label.probe.logical_processor_count"
    $totalMemory = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'total_physical_memory_bytes' -Label "$Label.probe") -ValueType UInt64 -Label "$Label.probe.total_physical_memory_bytes"
    $availableMemory = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'available_physical_memory_bytes' -Label "$Label.probe") -ValueType UInt64 -Label "$Label.probe.available_physical_memory_bytes"
    $elevated = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'elevated' -Label "$Label.probe") -ValueType Boolean -Label "$Label.probe.elevated"
    $tokenElevation = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'token_elevation_type' -Label "$Label.probe") -ValueType String -Label "$Label.probe.token_elevation_type"
    if (Test-StrictJsonProperty -Node $probe -Name 'cpu_busy_basis_points') {
        $null = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $probe -Name 'cpu_busy_basis_points' -Label "$Label.probe") -ValueType BasisPoints -Label "$Label.probe.cpu_busy_basis_points"
    }

    $volume = Get-StrictJsonProperty -Node $probe -Name 'output_volume' -Label "$Label.probe"
    $volumeFields = @('validated_output_path', 'volume_root', 'drive_type', 'filesystem', 'available_bytes_to_caller')
    Assert-StrictJsonObjectShape -Node $volume -Required $volumeFields -Allowed $volumeFields -Label "$Label.probe.output_volume"
    $validatedPath = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $volume -Name 'validated_output_path' -Label "$Label.probe.output_volume") -ValueType String -Label "$Label.probe.output_volume.validated_output_path"
    $volumeRoot = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $volume -Name 'volume_root' -Label "$Label.probe.output_volume") -ValueType String -Label "$Label.probe.output_volume.volume_root"
    foreach ($name in @('drive_type', 'filesystem')) {
        $null = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $volume -Name $name -Label "$Label.probe.output_volume") -ValueType String -Label "$Label.probe.output_volume.$name"
    }
    $null = Assert-ProbeFieldSchema -Node (Get-StrictJsonProperty -Node $volume -Name 'available_bytes_to_caller' -Label "$Label.probe.output_volume") -ValueType UInt64 -Label "$Label.probe.output_volume.available_bytes_to_caller"

    foreach ($field in @($osVersion, $osBuild, $nativeArchitecture, $processArchitecture)) {
        if ($field.State -cne 'KNOWN') {
            throw "$Label hard resolver probe field is not KNOWN."
        }
    }
    if ($osVersion.Value -cne (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'version' -Label $Label) -Label "$Label.version") -or
        $osBuild.Value -cne (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'build' -Label $Label) -Label "$Label.build") -or
        $nativeArchitecture.Value -cne (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'architecture' -Label $Label) -Label "$Label.architecture")) {
        throw "$Label compatibility mirrors disagree with probe values."
    }
    if ($logicalProcessors.State -ceq 'KNOWN' -and [UInt64]$logicalProcessors.Value -eq 0) {
        throw "$Label logical processor count must be positive."
    }
    if ($totalMemory.State -ceq 'KNOWN' -and [UInt64]$totalMemory.Value -eq 0) {
        throw "$Label total physical memory must be positive."
    }
    if ($totalMemory.State -ceq 'KNOWN' -and $availableMemory.State -ceq 'KNOWN' -and [UInt64]$availableMemory.Value -gt [UInt64]$totalMemory.Value) {
        throw "$Label available physical memory exceeds total physical memory."
    }
    if ($validatedPath.State -cne 'KNOWN' -or $volumeRoot.State -cne 'KNOWN') {
        throw "$Label output volume is not associated with a validated path."
    }
    if ($elevated.State -ceq 'KNOWN' -and (Test-StrictJsonProperty -Node $Node -Name 'elevated')) {
        $topElevated = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $Node -Name 'elevated' -Label $Label) -Label "$Label.elevated"
        if ([bool]$elevated.Value -ne $topElevated) {
            throw "$Label elevated mirror disagrees with the probe."
        }
    }
    if ($tokenElevation.State -ceq 'KNOWN') {
        if (@('default', 'full', 'limited') -cnotcontains [string]$tokenElevation.Value) {
            throw "$Label token elevation type is invalid."
        }
        if ($elevated.State -ceq 'KNOWN' -and (Test-StrictJsonProperty -Node $Node -Name 'privilege')) {
            $expectedPrivilege = if ([bool]$elevated.Value) { 'elevated' } elseif ($tokenElevation.Value -ceq 'limited') { 'filtered-admin' } else { 'standard-user' }
            $actualPrivilege = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'privilege' -Label $Label) -Label "$Label.privilege"
            if ($actualPrivilege -cne $expectedPrivilege) {
                throw "$Label privilege mirror disagrees with the probe."
            }
        }
    }
}

function Assert-ArtifactReferenceSchema {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $fields = @('path', 'media_type', 'content_schema_id', 'raw_or_derived', 'size', 'sha256')
    Assert-StrictJsonObjectShape -Node $Node -Required $fields -Allowed $fields -Label $Label
    $path = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'path' -Label $Label) -Label "$Label.path"
    $media = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'media_type' -Label $Label) -Label "$Label.media_type"
    $schema = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'content_schema_id' -Label $Label) -Label "$Label.content_schema_id"
    $classification = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'raw_or_derived' -Label $Label) -Label "$Label.raw_or_derived"
    $size = Get-StrictJsonInteger -Node (Get-StrictJsonProperty -Node $Node -Name 'size' -Label $Label) -Minimum 0 -Maximum ([Int64]::MaxValue) -Label "$Label.size"
    $sha256 = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'sha256' -Label $Label) -Label "$Label.sha256"
    if ($path -cne 'derived/process-identity-snapshot.ndjson' -or $media -cne 'application/x-ndjson' -or
        $schema -cne 'https://github.com/05wuyanzi/tannang/contracts/process-identity-snapshot-record-v0.schema.json' -or
        $classification -cne 'DERIVED' -or $sha256 -cnotmatch '^[0-9a-f]{64}$') {
        throw "$Label identity violates the committed artifact contract."
    }
    return [pscustomobject]@{ Node = $Node; Path = $path; Size = $size; SHA256 = $sha256 }
}

function Assert-FirstStagePackageMetadataSchema {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $required = @(
        'schema_version', 'manifest_version', 'product_version', 'runtime_artifact',
        'collection_id', 'started_at', 'finished_at', 'target_fingerprint', 'run_state',
        'receipt_references', 'artifact_references', 'directory_layout'
    )
    $allowed = $required + @('case_id', 'orchestration_reason')
    Assert-StrictJsonObjectShape -Node $Node -Required $required -Allowed $allowed -Label $Label
    if ((Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'schema_version' -Label $Label) -Label "$Label.schema_version") -cne '1.0' -or
        (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'manifest_version' -Label $Label) -Label "$Label.manifest_version") -cne '1.0' -or
        (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'runtime_artifact' -Label $Label) -Label "$Label.runtime_artifact") -cne 'tannang-first-stage') {
        throw "$Label identity is unsupported."
    }
    $productVersion = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'product_version' -Label $Label) -Label "$Label.product_version"
    $collectionID = Assert-CollectionID -Node (Get-StrictJsonProperty -Node $Node -Name 'collection_id' -Label $Label) -Label "$Label.collection_id"
    $caseID = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'case_id') {
        $caseNode = Get-StrictJsonProperty -Node $Node -Name 'case_id' -Label $Label
        Assert-StrictJsonStringLength -Node $caseNode -Minimum 1 -Maximum 128 -Label "$Label.case_id"
        $caseID = [string]$caseNode.Value
    }
    $startedAtNode = Get-StrictJsonProperty -Node $Node -Name 'started_at' -Label $Label
    $finishedAtNode = Get-StrictJsonProperty -Node $Node -Name 'finished_at' -Label $Label
    Assert-StrictJsonTimestamp -Node $startedAtNode -Label "$Label.started_at"
    Assert-StrictJsonTimestamp -Node $finishedAtNode -Label "$Label.finished_at"
    $startedAt = [DateTimeOffset]::Parse([string]$startedAtNode.Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind)
    $finishedAt = [DateTimeOffset]::Parse([string]$finishedAtNode.Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind)
    if ($finishedAt -lt $startedAt) {
        throw "$Label finish precedes start."
    }
    $fingerprint = Get-StrictJsonProperty -Node $Node -Name 'target_fingerprint' -Label $Label
    Assert-TargetFingerprintSchema -Node $fingerprint -Label "$Label.target_fingerprint"
    $runState = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $Node -Name 'run_state' -Label $Label) -Allowed @('COMPLETE', 'PARTIAL') -Label "$Label.run_state"
    if (Test-StrictJsonProperty -Node $Node -Name 'orchestration_reason') {
        $orchestration = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'orchestration_reason' -Label $Label) -Label "$Label.orchestration_reason"
        if ($orchestration -cne 'CANCELLED') {
            throw "$Label orchestration reason is invalid."
        }
    }

    $receiptArray = Get-StrictJsonProperty -Node $Node -Name 'receipt_references' -Label $Label
    if ($receiptArray.Kind -cne 'Array' -or $receiptArray.Value.Count -lt 1) {
        throw "$Label receipt_references must be a non-empty JSON array."
    }
    $receiptReferences = New-Object Collections.Generic.List[string]
    $seenReceipts = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($item in $receiptArray.Value) {
        $reference = Get-StrictJsonString -Node $item -Label "$Label.receipt_references"
        if ($reference -cnotmatch '^receipts/[A-Z0-9_]+\.json$' -or -not $seenReceipts.Add($reference)) {
            throw "$Label contains an unsafe or duplicate receipt reference."
        }
        $receiptReferences.Add($reference)
    }

    $artifactArray = Get-StrictJsonProperty -Node $Node -Name 'artifact_references' -Label $Label
    if ($artifactArray.Kind -cne 'Array' -or $artifactArray.Value.Count -gt 1) {
        throw "$Label artifact_references must be an array with at most one item."
    }
    $artifactReferences = New-Object Collections.Generic.List[object]
    foreach ($item in $artifactArray.Value) {
        $artifactReferences.Add((Assert-ArtifactReferenceSchema -Node $item -Label "$Label.artifact_references"))
    }

    $layoutArray = Get-StrictJsonProperty -Node $Node -Name 'directory_layout' -Label $Label
    $expectedLayout = @('meta', 'raw', 'derived', 'normalized', 'receipts', 'hashes', 'handoff', 'reports')
    if ($layoutArray.Kind -cne 'Array' -or $layoutArray.Value.Count -ne $expectedLayout.Count) {
        throw "$Label directory_layout must contain the exact committed package directories."
    }
    $layout = New-Object Collections.Generic.List[string]
    $seenLayout = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($item in $layoutArray.Value) {
        $name = Get-StrictJsonEnum -Node $item -Allowed $expectedLayout -Label "$Label.directory_layout"
        if (-not $seenLayout.Add($name)) {
            throw "$Label directory_layout contains a duplicate."
        }
        $layout.Add($name)
    }
    return [pscustomobject]@{
        Node = $Node
        ProductVersion = $productVersion
        CollectionID = $collectionID
        CaseID = $caseID
        StartedAt = [string]$startedAtNode.Value
        FinishedAt = [string]$finishedAtNode.Value
        Fingerprint = $fingerprint
        RunState = $runState
        ReceiptReferences = $receiptReferences
        ArtifactReferences = $artifactReferences
        DirectoryLayout = $layout
    }
}

function Assert-FirstStageReceiptSchema {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $required = @(
        'schema_version', 'manifest_version', 'product_version', 'runtime_artifact',
        'collection_id', 'target_fingerprint', 'requested_capability', 'compatibility',
        'attempted', 'execution', 'acquisition_started_at', 'acquisition_finished_at'
    )
    $allowed = $required + @(
        'case_id', 'capability', 'selected_provider', 'compatibility_reason',
        'candidate_evaluations', 'orchestration_reason', 'missing_evidence', 'artifact_reference'
    )
    Assert-StrictJsonObjectShape -Node $Node -Required $required -Allowed $allowed -Label $Label
    if ((Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'schema_version' -Label $Label) -Label "$Label.schema_version") -cne '1.0' -or
        (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'manifest_version' -Label $Label) -Label "$Label.manifest_version") -cne '1.0' -or
        (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'runtime_artifact' -Label $Label) -Label "$Label.runtime_artifact") -cne 'tannang-first-stage') {
        throw "$Label identity is unsupported."
    }
    $productVersion = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'product_version' -Label $Label) -Label "$Label.product_version"
    $collectionID = Assert-CollectionID -Node (Get-StrictJsonProperty -Node $Node -Name 'collection_id' -Label $Label) -Label "$Label.collection_id"
    $caseID = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'case_id') {
        $caseNode = Get-StrictJsonProperty -Node $Node -Name 'case_id' -Label $Label
        Assert-StrictJsonStringLength -Node $caseNode -Minimum 1 -Maximum 128 -Label "$Label.case_id"
        $caseID = [string]$caseNode.Value
    }
    $fingerprint = Get-StrictJsonProperty -Node $Node -Name 'target_fingerprint' -Label $Label
    Assert-TargetFingerprintSchema -Node $fingerprint -Label "$Label.target_fingerprint"

    $request = Get-StrictJsonProperty -Node $Node -Name 'requested_capability' -Label $Label
    $requestFields = @('id', 'priority', 'protected')
    Assert-StrictJsonObjectShape -Node $request -Required $requestFields -Allowed $requestFields -Label "$Label.requested_capability"
    $requestID = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $request -Name 'id' -Label "$Label.requested_capability") -Label "$Label.requested_capability.id"
    if ($requestID -cnotmatch '^[A-Z0-9_]+$') {
        throw "$Label requested capability ID is invalid."
    }
    $null = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $request -Name 'priority' -Label "$Label.requested_capability") -Allowed @('EARLY', 'NORMAL', 'LATE') -Label "$Label.requested_capability.priority"
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $request -Name 'protected' -Label "$Label.requested_capability") -Label "$Label.requested_capability.protected"

    $capability = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'capability') {
        $capability = Get-StrictJsonProperty -Node $Node -Name 'capability' -Label $Label
        $capabilityFields = @('id', 'description', 'acquisition_semantics', 'sensitivity')
        Assert-StrictJsonObjectShape -Node $capability -Required $capabilityFields -Allowed $capabilityFields -Label "$Label.capability"
        if ((Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $capability -Name 'id' -Label "$Label.capability") -Label "$Label.capability.id") -cne 'PROCESS_IDENTITY_SNAPSHOT' -or
            (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $capability -Name 'description' -Label "$Label.capability") -Label "$Label.capability.description") -cne 'Capture a minimal snapshot of visible Windows process identities: process ID, parent process ID, and executable name.' -or
            (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $capability -Name 'acquisition_semantics' -Label "$Label.capability") -Label "$Label.capability.acquisition_semantics") -cne 'STATE_SNAPSHOT' -or
            (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $capability -Name 'sensitivity' -Label "$Label.capability") -Label "$Label.capability.sensitivity") -cne 'medium') {
            throw "$Label capability does not match the fixed process identity snapshot definition."
        }
        if ($requestID -cne 'PROCESS_IDENTITY_SNAPSHOT') {
            throw "$Label capability does not match the request."
        }
    }

    $selectedProvider = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'selected_provider') {
        $selectedProvider = Get-StrictJsonProperty -Node $Node -Name 'selected_provider' -Label $Label
        Assert-StrictJsonObjectShape -Node $selectedProvider -Required @('id', 'class') -Allowed @('id', 'class') -Label "$Label.selected_provider"
        if ((Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $selectedProvider -Name 'id' -Label "$Label.selected_provider") -Label "$Label.selected_provider.id") -cne 'windows-toolhelp-process-snapshot' -or
            (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $selectedProvider -Name 'class' -Label "$Label.selected_provider") -Label "$Label.selected_provider.class") -cne 'FIRST_PARTY_NATIVE') {
            throw "$Label selected provider is invalid."
        }
        if ($null -eq $capability) {
            throw "$Label selected provider requires the fixed capability."
        }
    }
    $compatibility = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $Node -Name 'compatibility' -Label $Label) -Allowed @('AVAILABLE', 'DEGRADED', 'UNAVAILABLE') -Label "$Label.compatibility"
    if ($null -eq $selectedProvider -and $compatibility -cne 'UNAVAILABLE') {
        throw "$Label missing selected provider requires UNAVAILABLE compatibility."
    }
    if ($null -ne $selectedProvider -and $compatibility -ceq 'UNAVAILABLE') {
        throw "$Label selected provider cannot be UNAVAILABLE."
    }
    $reasonVocabulary = @('NONE', 'PRIVILEGE_REQUIRED', 'API_UNAVAILABLE', 'DEPENDENCY_MISSING', 'TARGET_STATE_RESTRICTED', 'TIMEOUT', 'CANCELLED', 'PROVIDER_ERROR', 'POLICY_DISABLED', 'UNSUPPORTED_OS', 'UNSUPPORTED_ARCH')
    $compatibilityReason = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'compatibility_reason') {
        $compatibilityReason = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $Node -Name 'compatibility_reason' -Label $Label) -Allowed $reasonVocabulary -Label "$Label.compatibility_reason"
    }

    $evaluations = New-Object Collections.Generic.List[object]
    if (Test-StrictJsonProperty -Node $Node -Name 'candidate_evaluations') {
        $evaluationArray = Get-StrictJsonProperty -Node $Node -Name 'candidate_evaluations' -Label $Label
        if ($evaluationArray.Kind -cne 'Array' -or $evaluationArray.Value.Count -gt 1) {
            throw "$Label candidate_evaluations must contain at most one item."
        }
        foreach ($evaluation in $evaluationArray.Value) {
            $fields = @('provider_id', 'compatibility', 'reason', 'eligible', 'score')
            Assert-StrictJsonObjectShape -Node $evaluation -Required $fields -Allowed $fields -Label "$Label.candidate_evaluations"
            $providerID = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $evaluation -Name 'provider_id' -Label "$Label.candidate_evaluations") -Label "$Label.candidate_evaluations.provider_id"
            $evaluationCompatibility = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $evaluation -Name 'compatibility' -Label "$Label.candidate_evaluations") -Allowed @('AVAILABLE', 'DEGRADED', 'UNAVAILABLE') -Label "$Label.candidate_evaluations.compatibility"
            $evaluationReason = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $evaluation -Name 'reason' -Label "$Label.candidate_evaluations") -Allowed $reasonVocabulary -Label "$Label.candidate_evaluations.reason"
            $eligible = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $evaluation -Name 'eligible' -Label "$Label.candidate_evaluations") -Label "$Label.candidate_evaluations.eligible"
            $score = Get-StrictJsonInteger -Node (Get-StrictJsonProperty -Node $evaluation -Name 'score' -Label "$Label.candidate_evaluations") -Minimum ([Int64]::MinValue) -Maximum ([Int64]::MaxValue) -Label "$Label.candidate_evaluations.score"
            if ($providerID -cne 'windows-toolhelp-process-snapshot') {
                throw "$Label candidate provider is invalid."
            }
            $evaluations.Add([pscustomobject]@{ Compatibility = $evaluationCompatibility; Reason = $evaluationReason; Eligible = $eligible; Score = $score })
        }
    }

    $attempted = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $Node -Name 'attempted' -Label $Label) -Label "$Label.attempted"
    $execution = Get-StrictJsonProperty -Node $Node -Name 'execution' -Label $Label
    Assert-StrictJsonObjectShape -Node $execution -Required @('state', 'reason', 'side_effect_summary') -Allowed @('state', 'reason', 'detail', 'side_effect_summary') -Label "$Label.execution"
    $executionState = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $execution -Name 'state' -Label "$Label.execution") -Allowed @('COLLECTED', 'PARTIAL', 'SKIPPED', 'FAILED', 'BLOCKED') -Label "$Label.execution.state"
    $executionReason = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $execution -Name 'reason' -Label "$Label.execution") -Allowed $reasonVocabulary -Label "$Label.execution.reason"
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $execution -Name 'side_effect_summary' -Label "$Label.execution") -Label "$Label.execution.side_effect_summary"
    if (Test-StrictJsonProperty -Node $execution -Name 'detail') {
        $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $execution -Name 'detail' -Label "$Label.execution") -Label "$Label.execution.detail" -AllowEmpty
    }

    if ($null -ne $selectedProvider) {
        if ($null -eq $compatibilityReason) {
            throw "$Label selected provider requires compatibility_reason."
        }
        if ($evaluations.Count -ne 1 -or -not $evaluations[0].Eligible -or
            $evaluations[0].Compatibility -cne $compatibility -or $evaluations[0].Reason -cne $compatibilityReason) {
            throw "$Label selected provider evaluation is inconsistent."
        }
        if (($compatibility -ceq 'AVAILABLE' -and $compatibilityReason -cne 'NONE') -or
            ($compatibility -ceq 'DEGRADED' -and $compatibilityReason -cne 'PRIVILEGE_REQUIRED')) {
            throw "$Label selected provider compatibility relation is invalid."
        }
    }
    if (-not $attempted -and $executionState -cne 'SKIPPED') {
        throw "$Label non-attempted receipt must use SKIPPED execution."
    }
    if ($attempted -and ($null -eq $capability -or $null -eq $selectedProvider -or $null -eq $compatibilityReason -or $evaluations.Count -ne 1 -or $requestID -cne 'PROCESS_IDENTITY_SNAPSHOT')) {
        throw "$Label attempted receipt is missing its fixed capability decision."
    }

    $orchestrationReason = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'orchestration_reason') {
        $orchestrationReason = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $Node -Name 'orchestration_reason' -Label $Label) -Allowed @('UNKNOWN_CAPABILITY', 'RESOLUTION_FAILED', 'CANCELLED', 'PACKAGE_FINALIZATION_FAILED') -Label "$Label.orchestration_reason"
        if ($attempted -or $executionReason -cne 'NONE') {
            throw "$Label orchestration accounting is invalid."
        }
    }
    $missingEvidence = New-Object Collections.Generic.List[string]
    if (Test-StrictJsonProperty -Node $Node -Name 'missing_evidence') {
        $missingArray = Get-StrictJsonProperty -Node $Node -Name 'missing_evidence' -Label $Label
        if ($missingArray.Kind -cne 'Array') {
            throw "$Label missing_evidence must be an array."
        }
        foreach ($item in $missingArray.Value) {
            $missingEvidence.Add((Get-StrictJsonString -Node $item -Label "$Label.missing_evidence"))
        }
    }

    $artifact = $null
    if (Test-StrictJsonProperty -Node $Node -Name 'artifact_reference') {
        $artifact = Assert-ArtifactReferenceSchema -Node (Get-StrictJsonProperty -Node $Node -Name 'artifact_reference' -Label $Label) -Label "$Label.artifact_reference"
        if (-not $attempted -or @('COLLECTED', 'PARTIAL') -cnotcontains $executionState) {
            throw "$Label artifact reference requires a retainable execution."
        }
    }
    elseif (@('COLLECTED', 'PARTIAL') -ccontains $executionState) {
        throw "$Label retainable execution requires an artifact reference."
    }
    if ($orchestrationReason -ceq 'PACKAGE_FINALIZATION_FAILED') {
        $expectedMissing = 'The requested evidence is absent because package staging preparation failed before Provider execution.'
        if ($attempted -or $executionState -cne 'SKIPPED' -or $executionReason -cne 'NONE' -or
            $null -eq $selectedProvider -or $null -eq $capability -or $evaluations.Count -ne 1 -or
            $missingEvidence.Count -ne 1 -or $missingEvidence[0] -cne $expectedMissing -or $null -ne $artifact) {
            throw "$Label package-finalization accounting is invalid."
        }
    }

    $startedAtNode = Get-StrictJsonProperty -Node $Node -Name 'acquisition_started_at' -Label $Label
    $finishedAtNode = Get-StrictJsonProperty -Node $Node -Name 'acquisition_finished_at' -Label $Label
    Assert-StrictJsonTimestamp -Node $startedAtNode -Label "$Label.acquisition_started_at"
    Assert-StrictJsonTimestamp -Node $finishedAtNode -Label "$Label.acquisition_finished_at"
    $startedAt = [DateTimeOffset]::Parse([string]$startedAtNode.Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind)
    $finishedAt = [DateTimeOffset]::Parse([string]$finishedAtNode.Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind)
    if ($finishedAt -lt $startedAt) {
        throw "$Label acquisition finish precedes start."
    }
    return [pscustomobject]@{
        Node = $Node
        ProductVersion = $productVersion
        CollectionID = $collectionID
        CaseID = $caseID
        Fingerprint = $fingerprint
        RequestID = $requestID
        Compatibility = $compatibility
        Attempted = $attempted
        ExecutionState = $executionState
        ExecutionReason = $executionReason
        Artifact = $artifact
        StartedAt = [string]$startedAtNode.Value
        FinishedAt = [string]$finishedAtNode.Value
    }
}

function Assert-HandoffStatusSchema {
    param(
        [Parameter(Mandatory = $true)]$Node,
        [Parameter(Mandatory = $true)][string]$Label
    )

    $fields = @('schema_version', 'prepared', 'executed', 'reason')
    Assert-StrictJsonObjectShape -Node $Node -Required $fields -Allowed $fields -Label $Label
    if ((Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'schema_version' -Label $Label) -Label "$Label.schema_version") -cne '1.0' -or
        (Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $Node -Name 'prepared' -Label $Label) -Label "$Label.prepared") -or
        (Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $Node -Name 'executed' -Label $Label) -Label "$Label.executed")) {
        throw "$Label identity is invalid."
    }
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $Node -Name 'reason' -Label $Label) -Label "$Label.reason"
}

function Assert-WindowsPEAmd64 {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    $reader = New-Object IO.BinaryReader($stream)
    try {
        if ($stream.Length -lt 64 -or $reader.ReadUInt16() -ne 0x5A4D) {
            throw "tannang.exe is not a DOS/PE image."
        }
        $stream.Position = 0x3C
        $peOffset = $reader.ReadInt32()
        if ($peOffset -lt 64 -or ($peOffset + 6) -gt $stream.Length) {
            throw "tannang.exe has an invalid PE header offset."
        }
        $stream.Position = $peOffset
        if ($reader.ReadUInt32() -ne 0x00004550) {
            throw "tannang.exe does not contain a PE signature."
        }
        if ($reader.ReadUInt16() -ne 0x8664) {
            throw "tannang.exe is not PE amd64."
        }
    }
    finally {
        $reader.Dispose()
        $stream.Dispose()
    }
}

function Assert-PortableManifest {
    param([Parameter(Mandatory = $true)][string]$PortableRoot)

    $manifestPath = Join-Path $PortableRoot 'SHA256SUMS.txt'
    $lines = [IO.File]::ReadAllLines($manifestPath)
    $expectedNames = @('tannang.exe', 'BUILD-INFO.json', 'LICENSE')
    if ($lines.Count -ne $expectedNames.Count) {
        throw "SHA256SUMS.txt must contain exactly three entries."
    }
    $seen = @{}
    for ($index = 0; $index -lt $lines.Count; $index++) {
        $match = [regex]::Match($lines[$index], '^([0-9a-f]{64})  ([A-Za-z0-9.-]+)$')
        if (-not $match.Success) {
            throw "SHA256SUMS.txt contains a malformed entry."
        }
        $name = $match.Groups[2].Value
        if ($name -cne $expectedNames[$index] -or $seen.ContainsKey($name)) {
            throw "SHA256SUMS.txt contains an unexpected or duplicate entry."
        }
        $seen[$name] = $true
        $actual = Get-Sha256 -Path (Join-Path $PortableRoot $name)
        if ($actual -cne $match.Groups[1].Value) {
            throw "SHA256SUMS.txt does not match $name."
        }
    }
}

function Get-RelativeForwardPath {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Path
    )

    $relative = $Path.Substring($Root.Length).TrimStart([char[]]@('\', '/'))
    return $relative.Replace('\', '/')
}

function Assert-CanonicalPackageRelativePath {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ([string]::IsNullOrEmpty($Path) -or $Path.Contains('\')) {
        throw "$Label must use package-relative forward slashes."
    }
    foreach ($component in $Path.Split('/')) {
        if ([string]::IsNullOrEmpty($component) -or $component -ceq '.' -or $component -ceq '..' -or
            $component.EndsWith(' ', [StringComparison]::Ordinal) -or $component.EndsWith('.', [StringComparison]::Ordinal)) {
            throw "$Label contains an ambiguous path component."
        }
        foreach ($character in $component.ToCharArray()) {
            if ([int]$character -lt 32 -or '<>:"/\|?*'.IndexOf($character) -ge 0) {
                throw "$Label contains a reserved Windows path character."
            }
        }
        $base = $component.Split('.')[0].ToUpperInvariant()
        $reserved = @('CON', 'CONIN$', 'CONOUT$', 'PRN', 'AUX', 'NUL', 'CLOCK$')
        if ($reserved -ccontains $base -or $base -cmatch '^(?:COM|LPT)[0-9]$') {
            throw "$Label contains a reserved DOS device name."
        }
        if ($base.Length -ge 3) {
            $last = $base[$base.Length - 1]
            if (($last -eq [char]0x00b9 -or $last -eq [char]0x00b2 -or $last -eq [char]0x00b3) -and
                ($base.Substring(0, $base.Length - 1) -ceq 'COM' -or $base.Substring(0, $base.Length - 1) -ceq 'LPT')) {
                throw "$Label contains a reserved DOS device name."
            }
        }
        $tilde = $base.LastIndexOf('~')
        if ($tilde -ge 1 -and $tilde -le 6 -and $tilde -lt ($base.Length - 1) -and $base.Substring($tilde + 1) -cmatch '^[0-9]+$') {
            throw "$Label resembles an ambiguous 8.3 alias."
        }
    }
}

function Assert-PackageManifest {
    param([Parameter(Mandatory = $true)][string]$PackageRoot)

    Assert-NoReparseTree -Path $PackageRoot
    $canonicalRoot = Get-CanonicalPath -Path $PackageRoot
    $manifestPath = Join-Path $canonicalRoot 'hashes\manifest.json'
    $manifest = Read-StrictJsonNode -Path $manifestPath -Label 'package manifest'
    $manifestFields = @('schema_version', 'algorithm', 'self_excluded', 'directories', 'entries')
    Assert-StrictJsonObjectShape -Node $manifest -Required $manifestFields -Allowed $manifestFields -Label 'package manifest'
    if ((Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $manifest -Name 'schema_version' -Label 'package manifest') -Label 'package manifest.schema_version') -cne '1.0' -or
        (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $manifest -Name 'algorithm' -Label 'package manifest') -Label 'package manifest.algorithm') -cne 'SHA-256' -or
        (Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $manifest -Name 'self_excluded' -Label 'package manifest') -Label 'package manifest.self_excluded') -cne 'hashes/manifest.json') {
        throw "Package manifest identity is invalid."
    }

    $entryArray = Get-StrictJsonProperty -Node $manifest -Name 'entries' -Label 'package manifest'
    if ($entryArray.Kind -cne 'Array') {
        throw "Package manifest entries must be a JSON array."
    }
    $declaredEntries = [Collections.Generic.Dictionary[string, object]]::new([StringComparer]::Ordinal)
    foreach ($entry in $entryArray.Value) {
        $entryFields = @('path', 'size', 'sha256')
        Assert-StrictJsonObjectShape -Node $entry -Required $entryFields -Allowed $entryFields -Label 'package manifest entry'
        $path = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $entry -Name 'path' -Label 'package manifest entry') -Label 'package manifest entry.path'
        Assert-CanonicalPackageRelativePath -Path $path -Label 'package manifest entry.path'
        if ($path -ceq 'hashes/manifest.json' -or $declaredEntries.ContainsKey($path)) {
            throw "Package manifest contains an unsafe or duplicate file path."
        }
        $size = Get-StrictJsonInteger -Node (Get-StrictJsonProperty -Node $entry -Name 'size' -Label 'package manifest entry') -Minimum 0 -Maximum ([Int64]::MaxValue) -Label 'package manifest entry.size'
        $sha256 = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $entry -Name 'sha256' -Label 'package manifest entry') -Label 'package manifest entry.sha256'
        if ($sha256 -cnotmatch '^[0-9a-f]{64}$') {
            throw "Package manifest entry identity is invalid."
        }
        $declaredEntries.Add($path, [pscustomobject]@{ Size = $size; SHA256 = $sha256 })
    }

    $actualFiles = @(
        Get-ChildItem -LiteralPath $canonicalRoot -Recurse -File -Force |
            ForEach-Object { Get-RelativeForwardPath -Root $canonicalRoot -Path $_.FullName } |
            Where-Object { $_ -cne 'hashes/manifest.json' }
    )
    if ($actualFiles.Count -ne $declaredEntries.Count) {
        throw "Package manifest file count does not match the package tree."
    }
    foreach ($relative in $actualFiles) {
        if (-not $declaredEntries.ContainsKey($relative)) {
            throw "Package contains an undeclared file."
        }
        $fullPath = Join-Path $canonicalRoot ($relative.Replace('/', '\'))
        $entry = $declaredEntries[$relative]
        if ((Get-Item -LiteralPath $fullPath).Length -ne $entry.Size -or (Get-Sha256 -Path $fullPath) -cne $entry.SHA256) {
            throw "Package manifest verification failed."
        }
    }

    $directoryArray = Get-StrictJsonProperty -Node $manifest -Name 'directories' -Label 'package manifest'
    if ($directoryArray.Kind -cne 'Array') {
        throw "Package manifest directories must be a JSON array."
    }
    $declaredDirectories = [Collections.Generic.Dictionary[string, Int64]]::new([StringComparer]::Ordinal)
    foreach ($directory in $directoryArray.Value) {
        $directoryFields = @('path', 'hashed_file_count')
        Assert-StrictJsonObjectShape -Node $directory -Required $directoryFields -Allowed $directoryFields -Label 'package manifest directory'
        $path = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $directory -Name 'path' -Label 'package manifest directory') -Label 'package manifest directory.path'
        $hashedFileCount = Get-StrictJsonInteger -Node (Get-StrictJsonProperty -Node $directory -Name 'hashed_file_count' -Label 'package manifest directory') -Minimum 0 -Maximum ([Int32]::MaxValue) -Label 'package manifest directory.hashed_file_count'
        Assert-CanonicalPackageRelativePath -Path $path -Label 'package manifest directory.path'
        if ($declaredDirectories.ContainsKey($path)) {
            throw "Package manifest contains an unsafe or duplicate directory."
        }
        $declaredDirectories.Add($path, $hashedFileCount)
    }
    $actualDirectories = @(Get-ChildItem -LiteralPath $canonicalRoot -Recurse -Directory -Force)
    if ($actualDirectories.Count -ne $declaredDirectories.Count) {
        throw "Package manifest directory count does not match the package tree."
    }
    foreach ($directory in $actualDirectories) {
        $relative = Get-RelativeForwardPath -Root $canonicalRoot -Path $directory.FullName
        if (-not $declaredDirectories.ContainsKey($relative)) {
            throw "Package contains an undeclared directory."
        }
        $directCount = @(
            Get-ChildItem -LiteralPath $directory.FullName -File -Force |
                Where-Object { (Get-RelativeForwardPath -Root $canonicalRoot -Path $_.FullName) -cne 'hashes/manifest.json' }
        ).Count
        if ($directCount -ne $declaredDirectories[$relative]) {
            throw "Package manifest directory file count does not match."
        }
    }
}

function Assert-ProcessArtifactSchema {
    param([Parameter(Mandatory = $true)][string]$Path)

    $rowCount = 0
    $reader = [IO.File]::OpenText($Path)
    try {
        while (($line = $reader.ReadLine()) -ne $null) {
            if ([string]::IsNullOrWhiteSpace($line)) {
                throw "Process snapshot contains an empty NDJSON row."
            }
            $row = ConvertFrom-StrictJsonText -Text $line -Label 'process snapshot row'
            $fields = @('process_id', 'parent_process_id', 'executable_name')
            Assert-StrictJsonObjectShape -Node $row -Required $fields -Allowed $fields -Label 'process snapshot row'
            $null = Get-StrictJsonUInt64 -Node (Get-StrictJsonProperty -Node $row -Name 'process_id' -Label 'process snapshot row') -Minimum 0 -Maximum ([UInt32]::MaxValue) -Label 'process snapshot row.process_id'
            $null = Get-StrictJsonUInt64 -Node (Get-StrictJsonProperty -Node $row -Name 'parent_process_id' -Label 'process snapshot row') -Minimum 0 -Maximum ([UInt32]::MaxValue) -Label 'process snapshot row.parent_process_id'
            $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $row -Name 'executable_name' -Label 'process snapshot row') -Label 'process snapshot row.executable_name'
            $rowCount++
        }
    }
    finally {
        $reader.Dispose()
    }
    if ($rowCount -eq 0) {
        throw "Retained process snapshot contains no complete records."
    }
    return $rowCount
}

function Assert-FirstStageEvidencePackageSchema {
    param(
        [Parameter(Mandatory = $true)][string]$PackageRoot,
        [string]$ExpectedProductVersion,
        [string]$ExpectedCaseId
    )

    Assert-NoReparseTree -Path $PackageRoot
    $canonicalRoot = Get-CanonicalPath -Path $PackageRoot
    $expectedDirectories = @('derived', 'handoff', 'hashes', 'meta', 'normalized', 'raw', 'receipts', 'reports')
    $actualDirectories = @(Get-ChildItem -LiteralPath $canonicalRoot -Recurse -Directory -Force | ForEach-Object { Get-RelativeForwardPath -Root $canonicalRoot -Path $_.FullName })
    [Array]::Sort($actualDirectories, [StringComparer]::Ordinal)
    if ($actualDirectories.Count -ne $expectedDirectories.Count) {
        throw "FirstStage package does not contain the exact committed directory layout."
    }
    for ($index = 0; $index -lt $expectedDirectories.Count; $index++) {
        if ($actualDirectories[$index] -cne $expectedDirectories[$index]) {
            throw "FirstStage package directory layout is invalid."
        }
    }
    $requiredFiles = @(
        'handoff/status.json',
        'hashes/manifest.json',
        'meta/package.json',
        'receipts/PROCESS_IDENTITY_SNAPSHOT.json'
    )
    $allowedFiles = $requiredFiles + @('derived/process-identity-snapshot.ndjson')
    $actualFiles = @(Get-ChildItem -LiteralPath $canonicalRoot -Recurse -File -Force | ForEach-Object { Get-RelativeForwardPath -Root $canonicalRoot -Path $_.FullName })
    [Array]::Sort($actualFiles, [StringComparer]::Ordinal)
    foreach ($path in $actualFiles) {
        if ($allowedFiles -cnotcontains $path) {
            throw "FirstStage package contains a file outside the committed layout."
        }
    }
    foreach ($path in $requiredFiles) {
        if ($actualFiles -cnotcontains $path) {
            throw "FirstStage package is missing a required package file."
        }
    }

    Assert-PackageManifest -PackageRoot $canonicalRoot
    $metadataNode = Read-StrictJsonNode -Path (Join-Path $canonicalRoot 'meta\package.json') -Label 'package metadata'
    $receiptNode = Read-StrictJsonNode -Path (Join-Path $canonicalRoot 'receipts\PROCESS_IDENTITY_SNAPSHOT.json') -Label 'FirstStage receipt'
    $handoffNode = Read-StrictJsonNode -Path (Join-Path $canonicalRoot 'handoff\status.json') -Label 'handoff status'
    $metadata = Assert-FirstStagePackageMetadataSchema -Node $metadataNode -Label 'package metadata'
    $receipt = Assert-FirstStageReceiptSchema -Node $receiptNode -Label 'FirstStage receipt'
    Assert-HandoffStatusSchema -Node $handoffNode -Label 'handoff status'
    $artifactPath = Join-Path $canonicalRoot 'derived\process-identity-snapshot.ndjson'
    $rowCount = 0

    if ($metadata.ReceiptReferences.Count -ne 1 -or $metadata.ReceiptReferences[0] -cne 'receipts/PROCESS_IDENTITY_SNAPSHOT.json' -or
        $receipt.RequestID -cne 'PROCESS_IDENTITY_SNAPSHOT') {
        throw "FirstStage package does not account for exactly the protected process identity capability."
    }
    if ($metadata.CollectionID -cne $receipt.CollectionID -or $metadata.CaseID -cne $receipt.CaseID -or
        $metadata.ProductVersion -cne $receipt.ProductVersion -or
        -not (Test-StrictJsonNodeEqual -Left $metadata.Fingerprint -Right $receipt.Fingerprint)) {
        throw "FirstStage package metadata and receipt are inconsistent."
    }
    if ($metadata.StartedAt -cne $receipt.StartedAt -or $metadata.FinishedAt -cne $receipt.FinishedAt) {
        throw "FirstStage package and receipt timestamps are inconsistent."
    }
    if (-not [string]::IsNullOrEmpty($ExpectedProductVersion) -and $metadata.ProductVersion -cne $ExpectedProductVersion) {
        throw "FirstStage package product version does not match the built artifact."
    }
    if (-not [string]::IsNullOrEmpty($ExpectedCaseId) -and $metadata.CaseID -cne $ExpectedCaseId) {
        throw "FirstStage package case ID does not match the acceptance invocation."
    }
    if ($null -ne $receipt.Artifact) {
        if ($metadata.ArtifactReferences.Count -ne 1 -or -not (Test-Path -LiteralPath $artifactPath -PathType Leaf)) {
            throw "FirstStage retained artifact is missing from package metadata or disk."
        }
        if (-not (Test-StrictJsonNodeEqual -Left $metadata.ArtifactReferences[0].Node -Right $receipt.Artifact.Node)) {
            throw "FirstStage package and receipt artifact references are inconsistent."
        }
        $rowCount = Assert-ProcessArtifactSchema -Path $artifactPath
        $artifact = Get-Item -LiteralPath $artifactPath -Force
        if ($artifact.Length -ne $receipt.Artifact.Size -or (Get-Sha256 -Path $artifactPath) -cne $receipt.Artifact.SHA256) {
            throw "FirstStage artifact bytes do not match the strict receipt reference."
        }
    }
    elseif ($metadata.ArtifactReferences.Count -ne 0 -or (Test-Path -LiteralPath $artifactPath)) {
        throw "FirstStage receipt-only package contains an undeclared artifact."
    }
    return [pscustomobject]@{
        Metadata = $metadata
        Receipt = $receipt
        ProcessRecordCount = $rowCount
    }
}

if ($MyInvocation.InvocationName -eq '.') {
    # Dot-sourcing loads the exact production validation helpers for bounded,
    # repo-external regression tests without entering the collection path.
    return
}

Assert-OrdinaryWindowsDriveAbsolutePath -Path $SourceRepository -Label 'SourceRepository'
Assert-OrdinaryWindowsDriveAbsolutePath -Path $AcceptanceRoot -Label 'AcceptanceRoot'
if ([string]::IsNullOrWhiteSpace($CaseId)) {
    throw "CaseId must be non-empty."
}

$sourceRoot = Get-CanonicalPath -Path $SourceRepository
$acceptanceRootPath = Get-CanonicalPath -Path $AcceptanceRoot
$acceptanceParent = [IO.Path]::GetDirectoryName($acceptanceRootPath)
if ([string]::IsNullOrWhiteSpace($acceptanceParent)) {
    throw "AcceptanceRoot must have a local parent directory."
}
Assert-OrdinaryWindowsDriveAbsolutePath -Path $acceptanceParent -Label 'AcceptanceRoot parent'
Assert-AcceptanceDriveTypeAllowed -DriveType (Get-AcceptanceDriveType -CanonicalPath $sourceRoot) -Label 'SourceRepository'
Assert-AcceptanceDriveTypeAllowed -DriveType (Get-AcceptanceDriveType -CanonicalPath $acceptanceParent) -Label 'AcceptanceRoot parent'

if (-not (Test-Path -LiteralPath $sourceRoot -PathType Container)) {
    throw "SourceRepository does not exist."
}
$reportedRoot = Invoke-GitCapture -Repository $sourceRoot -Arguments @('rev-parse', '--show-toplevel')
if (-not [string]::Equals((Get-CanonicalPath -Path $reportedRoot), $sourceRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "SourceRepository is not the exact Git repository root."
}
if (Test-Path -LiteralPath $acceptanceRootPath) {
    throw "AcceptanceRoot must not already exist."
}
if (-not (Test-Path -LiteralPath $acceptanceParent -PathType Container)) {
    throw "AcceptanceRoot parent must already exist."
}
if ([string]::Equals($acceptanceRootPath, $sourceRoot, [StringComparison]::OrdinalIgnoreCase) -or
    (Test-PathWithin -Child $acceptanceRootPath -Parent $sourceRoot)) {
    throw "AcceptanceRoot must be outside SourceRepository."
}
Assert-NoReparseAncestors -Path $acceptanceParent

$acceptanceId = Split-Path -Leaf $acceptanceRootPath
$expectedAcceptanceId = '^\d{8}T\d{6}Z-M5-WINDOWS-AMD64-' + [regex]::Escape($SourceRevision.Substring(0, 7)) + '$'
if ($acceptanceId -cnotmatch $expectedAcceptanceId) {
    throw "AcceptanceRoot basename does not match the acceptance ID contract."
}

$null = New-Item -ItemType Directory -Path $acceptanceRootPath
Assert-NoReparseAncestors -Path $acceptanceRootPath
$logsRoot = Join-Path $acceptanceRootPath 'logs'
$scratchRoot = Join-Path $acceptanceRootPath 'scratch'
$sourceClone = Join-Path $acceptanceRootPath 'source-clone'
$portableRoot = Join-Path $acceptanceRootPath 'portable'
$evidenceRoot = Join-Path $acceptanceRootPath 'evidence-package'
$summaryPath = Join-Path $acceptanceRootPath 'sanitized-summary.json'
$null = New-Item -ItemType Directory -Path $logsRoot
$null = New-Item -ItemType Directory -Path $scratchRoot

$realCollectionAttemptCount = 0
$cleanupWarnings = New-Object Collections.Generic.List[string]
$summaryJson = $null

try {
    $cloneOutput = @(& git clone --no-hardlinks --no-checkout -- $sourceRoot $sourceClone 2>&1)
    $cloneExit = $LASTEXITCODE
    Write-Utf8NoBom -Path (Join-Path $logsRoot 'source-clone.log') -Value ((($cloneOutput | ForEach-Object { $_.ToString() }) -join "`n") + "`n")
    if ($cloneExit -ne 0) {
        throw "Local no-hardlink source clone failed."
    }
    if (-not (Test-PathWithin -Child $sourceClone -Parent $acceptanceRootPath)) {
        throw "Source clone escaped AcceptanceRoot."
    }
    $null = Invoke-GitCapture -Repository $sourceClone -Arguments @('checkout', '--detach', $SourceRevision)
    $cloneHead = Invoke-GitCapture -Repository $sourceClone -Arguments @('rev-parse', 'HEAD')
    $cloneTree = Invoke-GitCapture -Repository $sourceClone -Arguments @('rev-parse', 'HEAD^{tree}')
    if ($cloneHead -cne $SourceRevision -or $cloneTree -cne $ExpectedSourceTree) {
        throw "Clean source clone identity does not match the acceptance source."
    }
    if ((Invoke-GitCapture -Repository $sourceClone -Arguments @('status', '--porcelain=v1', '--untracked-files=all')).Length -ne 0) {
        throw "Clean source clone contains tracked or ordinary untracked changes."
    }
    & git -C $sourceClone diff --cached --quiet --exit-code
    if ($LASTEXITCODE -ne 0) {
        throw "Clean source clone index is not empty."
    }
    if ((Invoke-GitCapture -Repository $sourceClone -Arguments @('ls-files', '--others', '--ignored', '--exclude-standard')).Length -ne 0) {
        throw "Clean source clone contains ignored untracked input."
    }

    $env:GOTOOLCHAIN = 'local'
    $env:GOPROXY = 'off'
    $hostExecutable = [Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
    if (-not (Test-Path -LiteralPath $hostExecutable -PathType Leaf)) {
        throw "Current PowerShell host executable could not be resolved."
    }
    $buildScript = Join-Path $sourceClone 'scripts\release\build-windows-amd64.ps1'
    Push-Location $scratchRoot
    try {
        $buildOutput = @(& $hostExecutable -NoProfile -ExecutionPolicy Bypass -File $buildScript -OutputDirectory $portableRoot -Mode RC 2>&1)
        $buildExit = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
    Write-Utf8NoBom -Path (Join-Path $logsRoot 'rc-build.log') -Value ((($buildOutput | ForEach-Object { $_.ToString() }) -join "`n") + "`n")
    if ($buildExit -ne 0) {
        throw "RC portable build failed."
    }

    Assert-NoReparseTree -Path $portableRoot
    $portableFiles = @(Get-ChildItem -LiteralPath $portableRoot -File -Force | ForEach-Object { $_.Name } | Sort-Object)
    $portableDirectories = @(Get-ChildItem -LiteralPath $portableRoot -Directory -Force)
    if ($portableDirectories.Count -ne 0 -or $portableFiles.Count -ne $expectedPortableFiles.Count) {
        throw "Portable artifact does not contain the exact four-file set."
    }
    for ($index = 0; $index -lt $expectedPortableFiles.Count; $index++) {
        if ($portableFiles[$index] -cne $expectedPortableFiles[$index]) {
            throw "Portable artifact file set is invalid."
        }
    }

    $binaryPath = Join-Path $portableRoot 'tannang.exe'
    $buildInfoPath = Join-Path $portableRoot 'BUILD-INFO.json'
    Assert-WindowsPEAmd64 -Path $binaryPath
    $buildInfoNode = Read-StrictJsonNode -Path $buildInfoPath -Label 'BUILD-INFO.json'
    $buildInfoFields = @('base_version', 'product_version', 'vcs', 'source_revision', 'source_modified', 'go_version', 'goos', 'goarch', 'binary_sha256', 'artifact_class', 'build_mode')
    Assert-StrictJsonObjectShape -Node $buildInfoNode -Required $buildInfoFields -Allowed $buildInfoFields -Label 'BUILD-INFO.json'
    foreach ($field in @('base_version', 'product_version', 'vcs', 'source_revision', 'go_version', 'goos', 'goarch', 'binary_sha256', 'artifact_class', 'build_mode')) {
        $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $buildInfoNode -Name $field -Label 'BUILD-INFO.json') -Label "BUILD-INFO.json.$field"
    }
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $buildInfoNode -Name 'source_modified' -Label 'BUILD-INFO.json') -Label 'BUILD-INFO.json.source_modified'
    $buildInfo = Read-JsonObject -Path $buildInfoPath
    Assert-ExactPropertySet -Value $buildInfo -Expected @('base_version', 'product_version', 'vcs', 'source_revision', 'source_modified', 'go_version', 'goos', 'goarch', 'binary_sha256', 'artifact_class', 'build_mode') -Label 'BUILD-INFO.json'
    if ($buildInfo.vcs -cne 'git' -or $buildInfo.source_revision -cne $SourceRevision -or [bool]$buildInfo.source_modified -or
        $buildInfo.goos -cne 'windows' -or $buildInfo.goarch -cne 'amd64' -or
        $buildInfo.artifact_class -cne $expectedArtifactClass -or $buildInfo.build_mode -cne 'RC' -or
        $buildInfo.binary_sha256 -cne (Get-Sha256 -Path $binaryPath)) {
        throw "BUILD-INFO.json does not match the clean RC acceptance source and artifact."
    }
    $productVersion = [string]$buildInfo.product_version
    if (-not $productVersion.StartsWith('0.0.0-pre-alpha+git.' + $SourceRevision, [StringComparison]::Ordinal)) {
        throw "BUILD-INFO.json product version does not bind the acceptance source."
    }
    if ((Get-Sha256 -Path (Join-Path $portableRoot 'LICENSE')) -cne (Get-Sha256 -Path (Join-Path $sourceClone 'LICENSE'))) {
        throw "Portable LICENSE bytes do not match the landed source."
    }
    Assert-PortableManifest -PortableRoot $portableRoot

    $versionErrorPath = Join-Path $logsRoot 'version.stderr.log'
    $versionOutput = @(& $binaryPath version 2> $versionErrorPath)
    $versionExit = $LASTEXITCODE
    $versionText = ($versionOutput | ForEach-Object { $_.ToString() }) -join "`n"
    Write-Utf8NoBom -Path (Join-Path $logsRoot 'version.stdout.log') -Value ($versionText + "`n")
    if ($versionExit -ne 0 -or (Get-Item -LiteralPath $versionErrorPath).Length -ne 0) {
        throw "Built version command failed or wrote stderr."
    }
    $versionNode = ConvertFrom-StrictJsonText -Text $versionText -Label 'tannang.exe version'
    $versionFields = @('base_version', 'product_version', 'vcs', 'source_revision', 'source_modified', 'go_version', 'goos', 'goarch')
    Assert-StrictJsonObjectShape -Node $versionNode -Required $versionFields -Allowed $versionFields -Label 'version JSON'
    foreach ($field in @('base_version', 'product_version', 'vcs', 'source_revision', 'go_version', 'goos', 'goarch')) {
        $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $versionNode -Name $field -Label 'version JSON') -Label "version JSON.$field"
    }
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $versionNode -Name 'source_modified' -Label 'version JSON') -Label 'version JSON.source_modified'
    $version = ConvertFrom-JsonObjectText -Value $versionText -Label 'tannang.exe version'
    Assert-ExactPropertySet -Value $version -Expected @('base_version', 'product_version', 'vcs', 'source_revision', 'source_modified', 'go_version', 'goos', 'goarch') -Label 'version JSON'
    foreach ($field in @('base_version', 'product_version', 'vcs', 'source_revision', 'source_modified', 'go_version', 'goos', 'goarch')) {
        if ($version.$field -cne $buildInfo.$field) {
            throw "Built version identity does not match BUILD-INFO.json."
        }
    }

    $portableHashes = [ordered]@{}
    foreach ($name in $expectedPortableFiles) {
        $portableHashes[$name] = Get-Sha256 -Path (Join-Path $portableRoot $name)
    }

    if (-not [Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([Runtime.InteropServices.OSPlatform]::Windows)) {
        throw "Acceptance host is not Windows."
    }
    $osArchitecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToUpperInvariant()
    $processArchitecture = [Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString().ToUpperInvariant()
    if ($osArchitecture -cne 'X64' -or $processArchitecture -cne 'X64') {
        throw "Acceptance host and process must both be Windows amd64."
    }
    $currentVersion = Get-ItemProperty -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
    $os = Get-CimInstance -ClassName Win32_OperatingSystem -Property Caption, Version, BuildNumber, OSArchitecture
    $osProduct = [string]$os.Caption
    $editionId = [string]$currentVersion.EditionID
    $displayVersion = [string]$currentVersion.DisplayVersion
    $osVersion = [string]$os.Version
    $build = [string]$os.BuildNumber
    $ubr = if ($null -eq $currentVersion.UBR) { 'UNKNOWN' } else { [string]$currentVersion.UBR }
    if ([string]::IsNullOrWhiteSpace($osProduct) -or [string]::IsNullOrWhiteSpace($editionId) -or
        [string]::IsNullOrWhiteSpace($displayVersion) -or [string]::IsNullOrWhiteSpace($osVersion) -or
        [string]::IsNullOrWhiteSpace($build)) {
        throw "Required sanitized Windows provenance is unavailable."
    }
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    $privilegeContext = if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { 'ELEVATED_ADMINISTRATOR' } else { 'NON_ELEVATED' }
    $identity.Dispose()

    $attemptMarker = [ordered]@{
        acceptance_contract = $acceptanceContract
        acceptance_id = $acceptanceId
        real_collection_attempted = $true
        real_collection_attempt_count = 1
        source_revision = $SourceRevision
        collection_scope = $expectedCapability
    }
    $attemptMarkerJson = ($attemptMarker | ConvertTo-Json -Depth 3) + "`n"
    Write-Utf8NoBom -Path (Join-Path $logsRoot 'real-collection-attempt.json') -Value $attemptMarkerJson
    $realCollectionAttemptCount = 1

    $collectErrorPath = Join-Path $logsRoot 'collect.stderr.log'
    $collectOutput = @(& $binaryPath collect --output $evidenceRoot --case-id $CaseId 2> $collectErrorPath)
    $collectExit = $LASTEXITCODE
    $collectText = ($collectOutput | ForEach-Object { $_.ToString() }) -join "`n"
    Write-Utf8NoBom -Path (Join-Path $logsRoot 'collect.stdout.log') -Value ($collectText + "`n")
    $collectNode = ConvertFrom-StrictJsonText -Text $collectText -Label 'tannang.exe collect'
    $allowedCollectFields = @('collection_id', 'case_id', 'run_state', 'orchestration_reason', 'finalization_verified', 'package_reference', 'capabilities')
    $requiredCollectFields = @('collection_id', 'case_id', 'run_state', 'finalization_verified', 'package_reference', 'capabilities')
    Assert-StrictJsonObjectShape -Node $collectNode -Required $requiredCollectFields -Allowed $allowedCollectFields -Label 'collect summary'
    $null = Assert-CollectionID -Node (Get-StrictJsonProperty -Node $collectNode -Name 'collection_id' -Label 'collect summary') -Label 'collect summary.collection_id'
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $collectNode -Name 'case_id' -Label 'collect summary') -Label 'collect summary.case_id'
    $null = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $collectNode -Name 'run_state' -Label 'collect summary') -Allowed @('COMPLETE', 'PARTIAL', 'FAILED') -Label 'collect summary.run_state'
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $collectNode -Name 'finalization_verified' -Label 'collect summary') -Label 'collect summary.finalization_verified'
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $collectNode -Name 'package_reference' -Label 'collect summary') -Label 'collect summary.package_reference'
    if (Test-StrictJsonProperty -Node $collectNode -Name 'orchestration_reason') {
        $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $collectNode -Name 'orchestration_reason' -Label 'collect summary') -Label 'collect summary.orchestration_reason'
    }
    $capabilityNodes = Get-StrictJsonProperty -Node $collectNode -Name 'capabilities' -Label 'collect summary'
    if ($capabilityNodes.Kind -cne 'Array' -or $capabilityNodes.Value.Count -ne 1) {
        throw "Collect summary must contain exactly one capability object."
    }
    $capabilityNode = $capabilityNodes.Value[0]
    $requiredCapabilityFields = @('id', 'protected', 'compatibility', 'attempted', 'execution_state', 'execution_reason', 'receipt_reference', 'artifact_reference')
    $allowedCapabilityFields = $requiredCapabilityFields + @('missing_evidence')
    Assert-StrictJsonObjectShape -Node $capabilityNode -Required $requiredCapabilityFields -Allowed $allowedCapabilityFields -Label 'collect capability summary'
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $capabilityNode -Name 'id' -Label 'collect capability summary') -Label 'collect capability summary.id'
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $capabilityNode -Name 'protected' -Label 'collect capability summary') -Label 'collect capability summary.protected'
    $null = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $capabilityNode -Name 'compatibility' -Label 'collect capability summary') -Allowed @('AVAILABLE', 'DEGRADED', 'UNAVAILABLE') -Label 'collect capability summary.compatibility'
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $capabilityNode -Name 'attempted' -Label 'collect capability summary') -Label 'collect capability summary.attempted'
    $null = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $capabilityNode -Name 'execution_state' -Label 'collect capability summary') -Allowed @('COLLECTED', 'PARTIAL', 'SKIPPED', 'FAILED', 'BLOCKED') -Label 'collect capability summary.execution_state'
    $null = Get-StrictJsonEnum -Node (Get-StrictJsonProperty -Node $capabilityNode -Name 'execution_reason' -Label 'collect capability summary') -Allowed @('NONE', 'PRIVILEGE_REQUIRED', 'API_UNAVAILABLE', 'DEPENDENCY_MISSING', 'TARGET_STATE_RESTRICTED', 'TIMEOUT', 'CANCELLED', 'PROVIDER_ERROR', 'POLICY_DISABLED', 'UNSUPPORTED_OS', 'UNSUPPORTED_ARCH') -Label 'collect capability summary.execution_reason'
    foreach ($field in @('receipt_reference', 'artifact_reference')) {
        $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $capabilityNode -Name $field -Label 'collect capability summary') -Label "collect capability summary.$field"
    }
    if (Test-StrictJsonProperty -Node $capabilityNode -Name 'missing_evidence') {
        $missingNode = Get-StrictJsonProperty -Node $capabilityNode -Name 'missing_evidence' -Label 'collect capability summary'
        if ($missingNode.Kind -cne 'Array') {
            throw "Collect capability missing_evidence must be an array."
        }
        foreach ($item in $missingNode.Value) {
            $null = Get-StrictJsonString -Node $item -Label 'collect capability missing_evidence'
        }
    }
    $collectSummary = ConvertFrom-JsonObjectText -Value $collectText -Label 'tannang.exe collect'
    foreach ($field in @($collectSummary.PSObject.Properties.Name)) {
        if ($allowedCollectFields -cnotcontains $field) {
            throw "Collect summary contains an unexpected field."
        }
    }
    if ([string]::IsNullOrWhiteSpace([string]$collectSummary.collection_id) -or $collectSummary.case_id -cne $CaseId -or
        [bool]$collectSummary.finalization_verified -ne $true -or $collectSummary.package_reference -cne $evidenceRoot) {
        throw "Collect summary identity or finalization is invalid."
    }
    $capabilities = @($collectSummary.capabilities)
    if ($capabilities.Count -ne 1) {
        throw "Real collection did not account for exactly one capability."
    }
    $capability = $capabilities[0]
    Assert-AllowedPropertySet -Value $capability `
        -Required @('id', 'protected', 'compatibility', 'attempted', 'execution_state', 'execution_reason', 'receipt_reference', 'artifact_reference') `
        -Allowed @('id', 'protected', 'compatibility', 'attempted', 'execution_state', 'execution_reason', 'missing_evidence', 'receipt_reference', 'artifact_reference') `
        -Label 'real capability summary'
    if ($capability.id -cne $expectedCapability -or -not [bool]$capability.protected -or -not [bool]$capability.attempted) {
        throw "Real collection scope is not the protected process identity baseline."
    }

    $strictPackage = Assert-FirstStageEvidencePackageSchema -PackageRoot $evidenceRoot -ExpectedProductVersion $productVersion -ExpectedCaseId $CaseId
    $packageMetadata = Read-JsonObject -Path (Join-Path $evidenceRoot 'meta\package.json')
    $receiptPath = Join-Path $evidenceRoot 'receipts\PROCESS_IDENTITY_SNAPSHOT.json'
    $receipt = Read-JsonObject -Path $receiptPath
    if ($packageMetadata.collection_id -cne $collectSummary.collection_id -or $receipt.collection_id -cne $collectSummary.collection_id -or
        $packageMetadata.case_id -cne $CaseId -or $receipt.case_id -cne $CaseId -or
        $packageMetadata.product_version -cne $buildInfo.product_version -or $receipt.product_version -cne $buildInfo.product_version -or
        $packageMetadata.runtime_artifact -cne 'tannang-first-stage' -or $receipt.runtime_artifact -cne 'tannang-first-stage' -or
        $packageMetadata.run_state -cne $collectSummary.run_state) {
        throw "Package, receipt, build, and collect metadata are inconsistent."
    }
    if (@($packageMetadata.receipt_references).Count -ne 1 -or $packageMetadata.receipt_references[0] -cne 'receipts/PROCESS_IDENTITY_SNAPSHOT.json' -or
        $receipt.requested_capability.id -cne $expectedCapability -or $receipt.capability.id -cne $expectedCapability -or
        $receipt.selected_provider.id -cne 'windows-toolhelp-process-snapshot' -or $receipt.attempted -ne $true -or
        $receipt.compatibility -cne $capability.compatibility -or $receipt.execution.state -cne $capability.execution_state -or
        $receipt.execution.reason -cne $capability.execution_reason) {
        throw "Receipt does not match the single real capability summary."
    }
    if (@($packageMetadata.artifact_references).Count -ne 1 -or $receipt.artifact_reference.path -cne 'derived/process-identity-snapshot.ndjson' -or
        $receipt.artifact_reference.path -cne $packageMetadata.artifact_references[0].path -or
        $receipt.artifact_reference.sha256 -cne $packageMetadata.artifact_references[0].sha256 -or
        [int64]$receipt.artifact_reference.size -ne [int64]$packageMetadata.artifact_references[0].size) {
        throw "Package and receipt artifact references are inconsistent."
    }
    $artifactPath = Join-Path $evidenceRoot 'derived\process-identity-snapshot.ndjson'
    if ((Get-Sha256 -Path $artifactPath) -cne [string]$receipt.artifact_reference.sha256 -or
        (Get-Item -LiteralPath $artifactPath).Length -ne [int64]$receipt.artifact_reference.size) {
        throw "Process snapshot bytes do not match the receipt reference."
    }
    $verifyErrorPath = Join-Path $logsRoot 'verify.stderr.log'
    $verifyOutput = @(& $binaryPath verify $evidenceRoot 2> $verifyErrorPath)
    $verifyExit = $LASTEXITCODE
    $verifyText = ($verifyOutput | ForEach-Object { $_.ToString() }) -join "`n"
    Write-Utf8NoBom -Path (Join-Path $logsRoot 'verify.stdout.log') -Value ($verifyText + "`n")
    if ($verifyExit -ne 0 -or (Get-Item -LiteralPath $verifyErrorPath).Length -ne 0) {
        throw "Built package verify command failed."
    }
    $verifyNode = ConvertFrom-StrictJsonText -Text $verifyText -Label 'tannang.exe verify'
    Assert-StrictJsonObjectShape -Node $verifyNode -Required @('package_path', 'verified') -Allowed @('package_path', 'verified') -Label 'verify JSON'
    $null = Get-StrictJsonString -Node (Get-StrictJsonProperty -Node $verifyNode -Name 'package_path' -Label 'verify JSON') -Label 'verify JSON.package_path'
    $null = Get-StrictJsonBoolean -Node (Get-StrictJsonProperty -Node $verifyNode -Name 'verified' -Label 'verify JSON') -Label 'verify JSON.verified'
    $verifySummary = ConvertFrom-JsonObjectText -Value $verifyText -Label 'tannang.exe verify'
    Assert-ExactPropertySet -Value $verifySummary -Expected @('package_path', 'verified') -Label 'verify JSON'
    if ($verifySummary.package_path -cne $evidenceRoot -or -not [bool]$verifySummary.verified) {
        throw "Package verify output is inconsistent."
    }

    $acceptanceResult = 'TESTED_FAIL'
    $degradationCode = 'UNKNOWN'
    $degradationSummary = 'Acceptance contract violation; private logs retained.'
    $degradationContractValid = $false
    if ($collectExit -eq 0 -and $collectSummary.run_state -ceq 'COMPLETE' -and
        $capability.compatibility -ceq 'AVAILABLE' -and $capability.execution_state -ceq 'COLLECTED' -and
        $capability.execution_reason -ceq 'NONE') {
        $acceptanceResult = 'TESTED_PASS'
        $degradationCode = 'NONE'
        $degradationSummary = 'NONE'
        $degradationContractValid = $true
    }
    elseif ($collectExit -eq 10 -and $collectSummary.run_state -ceq 'PARTIAL') {
        $knownReasons = @('PROVIDER_ERROR', 'TIMEOUT', 'CANCELLED', 'API_UNAVAILABLE', 'DEPENDENCY_MISSING', 'TARGET_STATE_RESTRICTED', 'PRIVILEGE_REQUIRED')
        $candidateCode = if ($capability.execution_reason -cne 'NONE') { [string]$capability.execution_reason } else { [string]$receipt.compatibility_reason }
        if (($capability.execution_state -ceq 'PARTIAL' -or $capability.compatibility -ceq 'DEGRADED') -and $knownReasons -ccontains $candidateCode) {
            $acceptanceResult = 'TESTED_DEGRADED'
            $degradationCode = $candidateCode
            $degradationSummary = 'Contract-valid partial or degraded process identity snapshot; raw detail remains private.'
            $degradationContractValid = $true
        }
    }
    $validatedAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ss.fffffffZ')
    $sanitizedSummary = [ordered]@{
        acceptance_contract = $acceptanceContract
        acceptance_id = $acceptanceId
        validated_at_utc = $validatedAt
        os_product = $osProduct
        edition_id = $editionId
        display_version = $displayVersion
        os_version = $osVersion
        build = $build
        ubr = $ubr
        os_architecture = $osArchitecture
        process_architecture = $processArchitecture
        privilege_context = $privilegeContext
        source_revision = $SourceRevision
        source_tree = $ExpectedSourceTree
        artifact_class = $expectedArtifactClass
        build_mode = 'RC'
        source_modified = $false
        artifact_hashes = $portableHashes
        result = $acceptanceResult
        execution_status = [string]$capability.execution_state
        degradation_code = $degradationCode
        degradation_summary = $degradationSummary
        package_manifest_verified = $true
        verify_passed = $true
        collection_scope = $expectedCapability
        evidence_reference = $acceptanceId
    }
    $summaryJson = ($sanitizedSummary | ConvertTo-Json -Depth 5) + "`n"
    Write-Utf8NoBom -Path $summaryPath -Value $summaryJson
    if ($acceptanceResult -ceq 'TESTED_FAIL') {
        throw "Real acceptance did not satisfy TESTED_PASS or a known contract-valid TESTED_DEGRADED outcome."
    }
}
finally {
    foreach ($cleanupPath in @($sourceClone, $scratchRoot)) {
        if (Test-Path -LiteralPath $cleanupPath) {
            try {
                if (-not (Test-PathWithin -Child $cleanupPath -Parent $acceptanceRootPath)) {
                    throw "Cleanup target escaped AcceptanceRoot."
                }
                Remove-Item -LiteralPath $cleanupPath -Recurse -Force -ErrorAction Stop
            }
            catch {
                $cleanupWarnings.Add($_.Exception.Message)
            }
        }
    }
    if ($cleanupWarnings.Count -ne 0) {
        Write-Warning ("Acceptance cleanup warning: " + ($cleanupWarnings -join '; '))
    }
}

if ($realCollectionAttemptCount -ne 1) {
    throw "Acceptance did not perform exactly one real collection attempt."
}
if ($null -eq $summaryJson) {
    throw "Acceptance did not produce a sanitized summary."
}
Write-Output $summaryJson.TrimEnd([char[]]@("`r", "`n"))
