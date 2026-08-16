# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory,

    [ValidateSet('Development', 'RC')]
    [string]$Mode = 'RC'
)

$ErrorActionPreference = 'Stop'

function Invoke-GitCapture {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepositoryRoot,

        [Parameter(Mandatory = $true)]
        [string[]]$GitArguments
    )

    $result = @(& git -C $RepositoryRoot @GitArguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "git $($GitArguments -join ' ') failed: $($result -join [Environment]::NewLine)"
    }
    return ($result -join [Environment]::NewLine).Trim()
}

function Assert-NoReparseAncestor {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $item = Get-Item -LiteralPath $Path -Force
    while ($null -ne $item) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Output path traverses a reparse point."
        }
        $item = $item.Parent
    }
}

function Get-CanonicalDirectoryPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $fullPath = [IO.Path]::GetFullPath($Path)
    $pathRoot = [IO.Path]::GetPathRoot($fullPath)
    if ([string]::Equals($fullPath, $pathRoot, [StringComparison]::OrdinalIgnoreCase)) {
        return $pathRoot
    }
    return $fullPath.TrimEnd([char[]]@([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar))
}

function Invoke-VersionCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$BinaryPath
    )

    $startInfo = New-Object Diagnostics.ProcessStartInfo
    $startInfo.FileName = $BinaryPath
    $startInfo.Arguments = 'version'
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = New-Object Diagnostics.Process
    $process.StartInfo = $startInfo
    if (-not $process.Start()) {
        throw "Unable to start the built version command."
    }
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    if ($process.ExitCode -ne 0 -or $stderr.Length -ne 0) {
        throw "Built version command failed with exit code $($process.ExitCode)."
    }
    try {
        return $stdout | ConvertFrom-Json
    }
    catch {
        throw "Built version command did not return valid JSON: $($_.Exception.Message)"
    }
}

if (-not [IO.Path]::IsPathRooted($OutputDirectory) -or $OutputDirectory -match '^[A-Za-z]:[^\\/]') {
    throw "OutputDirectory must be an absolute path."
}

$repoRoot = Get-CanonicalDirectoryPath -Path (Join-Path $PSScriptRoot '..\..')
$reportedRoot = Invoke-GitCapture -RepositoryRoot $repoRoot -GitArguments @('rev-parse', '--show-toplevel')
if (-not [string]::Equals((Get-CanonicalDirectoryPath -Path $reportedRoot), $repoRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "The build script is not running from the expected repository root."
}

$outputRoot = Get-CanonicalDirectoryPath -Path $OutputDirectory
$repoPrefix = $repoRoot
if (-not $repoPrefix.EndsWith([string][IO.Path]::DirectorySeparatorChar, [StringComparison]::Ordinal)) {
    $repoPrefix += [IO.Path]::DirectorySeparatorChar
}
if ([string]::Equals($outputRoot, $repoRoot, [StringComparison]::OrdinalIgnoreCase) -or
    $outputRoot.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputDirectory must be outside the repository root."
}
if (Test-Path -LiteralPath $outputRoot) {
    throw "OutputDirectory already exists."
}
$outputParent = Split-Path -Parent $outputRoot
if (-not (Test-Path -LiteralPath $outputParent -PathType Container)) {
    throw "OutputDirectory parent must already exist."
}
Assert-NoReparseAncestor -Path $outputParent

$head = Invoke-GitCapture -RepositoryRoot $repoRoot -GitArguments @('rev-parse', 'HEAD')
if ($head -cnotmatch '^[0-9a-f]{40}$') {
    throw "HEAD is not a valid Git commit identity."
}
$status = Invoke-GitCapture -RepositoryRoot $repoRoot -GitArguments @('status', '--porcelain=v1', '--untracked-files=all')
$sourceDirty = $status.Length -ne 0

& git -C $repoRoot diff --cached --quiet --exit-code
$stagedExit = $LASTEXITCODE
if ($stagedExit -gt 1) {
    throw "Unable to determine staged state."
}
if ($Mode -eq 'RC') {
    $ignoredUntracked = Invoke-GitCapture -RepositoryRoot $repoRoot -GitArguments @('ls-files', '--others', '--ignored', '--exclude-standard')
    if ($sourceDirty -or $stagedExit -ne 0 -or $ignoredUntracked.Length -ne 0) {
        throw "RC mode requires a clean worktree, empty index, and zero ignored untracked files."
    }
}

$env:GOTOOLCHAIN = 'local'
$env:GOPROXY = 'off'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

$null = New-Item -ItemType Directory -Path $outputRoot
$binaryPath = Join-Path $outputRoot 'tannang.exe'
$licensePath = Join-Path $outputRoot 'LICENSE'
$buildInfoPath = Join-Path $outputRoot 'BUILD-INFO.json'
$manifestPath = Join-Path $outputRoot 'SHA256SUMS.txt'

Push-Location $repoRoot
try {
    & go build -trimpath -buildvcs=true -o $binaryPath ./cmd/tannang
    if ($LASTEXITCODE -ne 0) {
        throw "Windows amd64 build failed."
    }
}
finally {
    Pop-Location
}

Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE') -Destination $licensePath
$version = Invoke-VersionCommand -BinaryPath $binaryPath
$requiredVersionFields = @('base_version', 'product_version', 'vcs', 'source_revision', 'source_modified', 'go_version', 'goos', 'goarch')
$actualVersionFields = @($version.PSObject.Properties.Name)
if (@(Compare-Object $requiredVersionFields $actualVersionFields).Count -ne 0) {
    throw "Built version command returned an unexpected field set."
}
if ($version.vcs -ne 'git' -or $version.source_revision -cne $head -or $version.goos -ne 'windows' -or $version.goarch -ne 'amd64') {
    throw "Built version identity does not match the requested source and target."
}
if ($Mode -eq 'RC' -and [bool]$version.source_modified) {
    throw "RC build reported modified source."
}
if ($Mode -eq 'Development' -and $sourceDirty -and -not [bool]$version.source_modified) {
    throw "Development build did not report the dirty source state."
}

$binaryHash = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
$buildInfo = [ordered]@{
    base_version     = [string]$version.base_version
    product_version  = [string]$version.product_version
    vcs              = [string]$version.vcs
    source_revision  = [string]$version.source_revision
    source_modified  = [bool]$version.source_modified
    go_version       = [string]$version.go_version
    goos             = [string]$version.goos
    goarch           = [string]$version.goarch
    binary_sha256    = $binaryHash
    artifact_class   = 'HEADLESS_PORTABLE_WINDOWS_AMD64'
    build_mode       = $Mode.ToUpperInvariant()
}
$utf8WithoutBOM = New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($buildInfoPath, (($buildInfo | ConvertTo-Json -Depth 3) + "`n"), $utf8WithoutBOM)

$manifestNames = @('tannang.exe', 'BUILD-INFO.json', 'LICENSE')
$manifestLines = foreach ($name in $manifestNames) {
    $hash = (Get-FileHash -LiteralPath (Join-Path $outputRoot $name) -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $name"
}
[IO.File]::WriteAllText($manifestPath, (($manifestLines -join "`n") + "`n"), [Text.Encoding]::ASCII)

foreach ($index in 0..($manifestNames.Count - 1)) {
    $name = $manifestNames[$index]
    $actualHash = (Get-FileHash -LiteralPath (Join-Path $outputRoot $name) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($manifestLines[$index] -cne "$actualHash  $name") {
        throw "SHA256SUMS verification failed for $name."
    }
}
$expectedFiles = @('BUILD-INFO.json', 'LICENSE', 'SHA256SUMS.txt', 'tannang.exe')
$actualFiles = @(Get-ChildItem -LiteralPath $outputRoot -File | ForEach-Object { $_.Name } | Sort-Object)
$actualDirectories = @(Get-ChildItem -LiteralPath $outputRoot -Directory)
if ($actualDirectories.Count -ne 0 -or @(Compare-Object $expectedFiles $actualFiles).Count -ne 0) {
    throw "Portable artifact output does not contain the exact required file set."
}

$buildInfo | ConvertTo-Json -Depth 3
