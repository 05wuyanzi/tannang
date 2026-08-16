# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param()

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

function Write-UTF8WithoutBOM {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string]$Value
    )

    $encoding = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($Path, $Value, $encoding)
}

function Get-ActivePowerShellExecutable {
    try {
        $executable = [Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
    }
    catch {
        throw "Unable to resolve the active PowerShell executable: $($_.Exception.Message)"
    }
    if ([string]::IsNullOrWhiteSpace($executable) -or -not (Test-Path -LiteralPath $executable -PathType Leaf)) {
        throw "The active PowerShell executable could not be resolved to an existing file."
    }
    return [IO.Path]::GetFullPath($executable)
}

function Get-RepositoryState {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepositoryRoot
    )

    return [PSCustomObject]@{
        Head   = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('rev-parse', 'HEAD')
        Branch = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('branch', '--show-current')
        Refs   = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('for-each-ref', '--sort=refname', '--format=%(refname)|%(objectname)', 'refs/heads', 'refs/remotes')
        Staged = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('diff', '--cached', '--name-only')
        Status = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('status', '--porcelain=v1', '--untracked-files=all')
    }
}

function Assert-RepositoryStateEqual {
    param(
        [Parameter(Mandatory = $true)]
        [PSCustomObject]$Expected,

        [Parameter(Mandatory = $true)]
        [PSCustomObject]$Actual,

        [Parameter(Mandatory = $true)]
        [string]$Context
    )

    foreach ($property in @('Head', 'Branch', 'Refs', 'Staged', 'Status')) {
        if ($Expected.$property -cne $Actual.$property) {
            throw "$Context changed repository state field $property."
        }
    }
}

$script:activePowerShellExecutable = Get-ActivePowerShellExecutable

function Invoke-BuildHelper {
    param(
        [Parameter(Mandatory = $true)]
        [string]$HelperPath,

        [Parameter(Mandatory = $true)]
        [string]$OutputDirectory,

        [Parameter(Mandatory = $true)]
        [ValidateSet('Development', 'RC')]
        [string]$Mode,

        [Parameter(Mandatory = $true)]
        [string]$LogPath
    )

    $startInfo = New-Object Diagnostics.ProcessStartInfo
    $startInfo.FileName = $script:activePowerShellExecutable
    $startInfo.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$HelperPath`" -OutputDirectory `"$OutputDirectory`" -Mode $Mode"
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = New-Object Diagnostics.Process
    $process.StartInfo = $startInfo
    if (-not $process.Start()) {
        throw "Unable to start the candidate build helper."
    }
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    $exitCode = $process.ExitCode
    Write-UTF8WithoutBOM -Path $LogPath -Value ($stdout + $stderr)
    return [PSCustomObject]@{
        ExitCode = $exitCode
        Output   = $stdout + $stderr
    }
}

function New-IsolatedRepository {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SourceRepository,

        [Parameter(Mandatory = $true)]
        [string]$DestinationRepository
    )

    $parent = Split-Path -Parent $DestinationRepository
    & git -C $parent clone --quiet --no-hardlinks $SourceRepository $DestinationRepository
    if ($LASTEXITCODE -ne 0) {
        throw "Clone isolated repository failed: $DestinationRepository"
    }
    $status = Invoke-GitCapture -RepositoryRoot $DestinationRepository -GitArguments @('status', '--porcelain=v1', '--untracked-files=all')
    if ($status.Length -ne 0) {
        throw "Isolated repository did not start clean: $DestinationRepository"
    }
}

function Assert-DevelopmentBuildModified {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepositoryRoot,

        [Parameter(Mandatory = $true)]
        [string]$OutputDirectory,

        [Parameter(Mandatory = $true)]
        [string]$LogPath,

        [Parameter(Mandatory = $true)]
        [string]$Context
    )

    $helper = Join-Path $RepositoryRoot 'scripts\release\build-windows-amd64.ps1'
    $build = Invoke-BuildHelper -HelperPath $helper -OutputDirectory $OutputDirectory -Mode Development -LogPath $LogPath
    if ($build.ExitCode -ne 0) {
        throw "$Context Development build failed: $($build.Output)"
    }
    $buildInfo = Get-Content -Raw -LiteralPath (Join-Path $OutputDirectory 'BUILD-INFO.json') | ConvertFrom-Json
    if (-not [bool]$buildInfo.source_modified -or $buildInfo.build_mode -ne 'DEVELOPMENT') {
        throw "$Context Development build reported source_modified=$($buildInfo.source_modified), build_mode=$($buildInfo.build_mode)."
    }
}

function Assert-RCBuildRejected {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepositoryRoot,

        [Parameter(Mandatory = $true)]
        [string]$OutputDirectory,

        [Parameter(Mandatory = $true)]
        [string]$LogPath
    )

    $helper = Join-Path $RepositoryRoot 'scripts\release\build-windows-amd64.ps1'
    $build = Invoke-BuildHelper -HelperPath $helper -OutputDirectory $OutputDirectory -Mode RC -LogPath $LogPath
    if ($build.ExitCode -eq 0) {
        throw "RC build accepted a dirty or repository-contained input."
    }
    if (Test-Path -LiteralPath $OutputDirectory) {
        throw "Rejected RC build created its output directory."
    }
    return $build
}

function Assert-ManifestValid {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ArtifactRoot
    )

    $requiredNames = @('tannang.exe', 'BUILD-INFO.json', 'LICENSE')
    $lines = @([IO.File]::ReadAllLines((Join-Path $ArtifactRoot 'SHA256SUMS.txt')))
    if ($lines.Count -ne $requiredNames.Count) {
        throw "SHA256SUMS.txt has an unexpected entry count."
    }
    $entries = @{}
    foreach ($line in $lines) {
        if ($line -cnotmatch '^([0-9a-f]{64})  ([^\\/]+)$') {
            throw "SHA256SUMS.txt contains a malformed entry: $line"
        }
        $expectedHash = $Matches[1]
        $name = $Matches[2]
        if ($entries.ContainsKey($name)) {
            throw "SHA256SUMS.txt contains duplicate entry $name."
        }
        $path = Join-Path $ArtifactRoot $name
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "SHA256SUMS.txt references missing file $name."
        }
        $actualHash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($expectedHash -cne $actualHash) {
            throw "SHA256SUMS.txt hash mismatch for $name."
        }
        $entries[$name] = $true
    }
    foreach ($name in $requiredNames) {
        if (-not $entries.ContainsKey($name)) {
            throw "SHA256SUMS.txt is missing required entry $name."
        }
    }
}

function Assert-WindowsAMD64PE {
    param(
        [Parameter(Mandatory = $true)]
        [string]$BinaryPath
    )

    $bytes = [IO.File]::ReadAllBytes($BinaryPath)
    if ($bytes.Length -lt 64 -or $bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a) {
        throw "Built executable is not an MZ image."
    }
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3c)
    if ($peOffset -lt 0 -or $peOffset + 6 -gt $bytes.Length -or
        $bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45 -or
        $bytes[$peOffset + 2] -ne 0 -or $bytes[$peOffset + 3] -ne 0) {
        throw "Built executable has an invalid PE signature."
    }
    if ([BitConverter]::ToUInt16($bytes, $peOffset + 4) -ne 0x8664) {
        throw "Built executable is not PE amd64."
    }
}

function Assert-ExactIgnoredFile {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepositoryRoot,

        [Parameter(Mandatory = $true)]
        [string]$RelativePath
    )

    $ordinary = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('status', '--porcelain=v1', '--untracked-files=all')
    if ($ordinary -match [regex]::Escape($RelativePath)) {
        throw "Ordinary Git status unexpectedly reported ignored probe $RelativePath."
    }
    $ignored = Invoke-GitCapture -RepositoryRoot $RepositoryRoot -GitArguments @('ls-files', '--others', '--ignored', '--exclude-standard')
    if ($ignored -cne $RelativePath) {
        throw "Ignored-file enumeration returned $ignored, want exactly $RelativePath."
    }
}

$sourceRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$reportedRoot = Invoke-GitCapture -RepositoryRoot $sourceRoot -GitArguments @('rev-parse', '--show-toplevel')
if ([IO.Path]::GetFullPath($reportedRoot) -ne $sourceRoot) {
    throw "The regression test is not running from the expected repository root."
}
$sourceStateBefore = Get-RepositoryState -RepositoryRoot $sourceRoot

$tempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$tempRoot = [IO.Path]::GetFullPath((Join-Path $tempBase ('tannang-rc-ignored-test-' + [Guid]::NewGuid().ToString('N'))))
$sourcePrefix = $sourceRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
$tempPrefix = $tempBase.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
if ($tempRoot.StartsWith($sourcePrefix, [StringComparison]::OrdinalIgnoreCase) -or -not $tempRoot.StartsWith($tempPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Temporary regression root is not safely repo-external."
}

$result = $null
try {
    $fixtureRoot = Join-Path $tempRoot 'fixture'
    $null = New-Item -ItemType Directory -Path $fixtureRoot

    $snapshotPaths = @(& git -C $sourceRoot ls-files --cached --others --exclude-standard 2>&1)
    if ($LASTEXITCODE -ne 0 -or $snapshotPaths.Count -eq 0) {
        throw "Unable to enumerate the current candidate snapshot."
    }
    foreach ($relativePath in $snapshotPaths) {
        $sourcePath = Join-Path $sourceRoot $relativePath
        $destinationPath = [IO.Path]::GetFullPath((Join-Path $fixtureRoot $relativePath))
        $fixturePrefix = $fixtureRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
        if (-not $destinationPath.StartsWith($fixturePrefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Candidate snapshot path escapes the temporary fixture."
        }
        $destinationParent = Split-Path -Parent $destinationPath
        if (-not (Test-Path -LiteralPath $destinationParent -PathType Container)) {
            $null = New-Item -ItemType Directory -Path $destinationParent -Force
        }
        Copy-Item -LiteralPath $sourcePath -Destination $destinationPath
    }

    $sourceHelper = Join-Path $sourceRoot 'scripts\release\build-windows-amd64.ps1'
    $fixtureHelper = Join-Path $fixtureRoot 'scripts\release\build-windows-amd64.ps1'
    if ((Get-FileHash -LiteralPath $sourceHelper -Algorithm SHA256).Hash -cne (Get-FileHash -LiteralPath $fixtureHelper -Algorithm SHA256).Hash) {
        throw "Temporary fixture does not contain the current candidate helper bytes."
    }

    & git -C $fixtureRoot init --quiet
    if ($LASTEXITCODE -ne 0) { throw "Initialize temporary Git repository failed." }
    & git -C $fixtureRoot config user.name 'Tannang RC Repair Test'
    if ($LASTEXITCODE -ne 0) { throw "Configure temporary Git user name failed." }
    & git -C $fixtureRoot config user.email 'rc-repair-test@example.invalid'
    if ($LASTEXITCODE -ne 0) { throw "Configure temporary Git user email failed." }
    & git -C $fixtureRoot add --all
    if ($LASTEXITCODE -ne 0) { throw "Stage temporary fixture failed." }
    & git -C $fixtureRoot commit --quiet -m 'test: create RC provenance fixture'
    if ($LASTEXITCODE -ne 0) { throw "Commit temporary fixture failed." }
    $fixtureHead = Invoke-GitCapture -RepositoryRoot $fixtureRoot -GitArguments @('rev-parse', 'HEAD')

    $env:GOTOOLCHAIN = 'local'
    $env:GOPROXY = 'off'

    $trackedRoot = Join-Path $tempRoot 'tracked-modified'
    New-IsolatedRepository -SourceRepository $fixtureRoot -DestinationRepository $trackedRoot
    [IO.File]::AppendAllText((Join-Path $trackedRoot 'README.md'), "`ntracked modification probe`n")
    Assert-DevelopmentBuildModified -RepositoryRoot $trackedRoot -OutputDirectory (Join-Path $tempRoot 'dev-tracked-output') -LogPath (Join-Path $tempRoot 'dev-tracked.log') -Context 'Tracked modification'
    $null = Assert-RCBuildRejected -RepositoryRoot $trackedRoot -OutputDirectory (Join-Path $tempRoot 'rc-tracked-output') -LogPath (Join-Path $tempRoot 'rc-tracked.log')

    $stagedRoot = Join-Path $tempRoot 'staged-modified'
    New-IsolatedRepository -SourceRepository $fixtureRoot -DestinationRepository $stagedRoot
    [IO.File]::AppendAllText((Join-Path $stagedRoot 'README.md'), "`nstaged modification probe`n")
    & git -C $stagedRoot add -- README.md
    if ($LASTEXITCODE -ne 0) { throw "Stage isolated modification failed." }
    Assert-DevelopmentBuildModified -RepositoryRoot $stagedRoot -OutputDirectory (Join-Path $tempRoot 'dev-staged-output') -LogPath (Join-Path $tempRoot 'dev-staged.log') -Context 'Staged modification'
    $null = Assert-RCBuildRejected -RepositoryRoot $stagedRoot -OutputDirectory (Join-Path $tempRoot 'rc-staged-output') -LogPath (Join-Path $tempRoot 'rc-staged.log')

    $untrackedRoot = Join-Path $tempRoot 'ordinary-untracked'
    New-IsolatedRepository -SourceRepository $fixtureRoot -DestinationRepository $untrackedRoot
    Write-UTF8WithoutBOM -Path (Join-Path $untrackedRoot 'zz_rc_untracked_probe.txt') -Value "ordinary untracked probe`n"
    Assert-DevelopmentBuildModified -RepositoryRoot $untrackedRoot -OutputDirectory (Join-Path $tempRoot 'dev-untracked-output') -LogPath (Join-Path $tempRoot 'dev-untracked.log') -Context 'Ordinary untracked file'
    $null = Assert-RCBuildRejected -RepositoryRoot $untrackedRoot -OutputDirectory (Join-Path $tempRoot 'rc-untracked-output') -LogPath (Join-Path $tempRoot 'rc-untracked.log')

    $ignoredRoot = Join-Path $tempRoot 'ignored-untracked'
    New-IsolatedRepository -SourceRepository $fixtureRoot -DestinationRepository $ignoredRoot
    $goProbeRelative = 'internal/buildinfo/zz_rc_ignored_probe.go'
    $goProbePath = Join-Path $ignoredRoot ($goProbeRelative -replace '/', '\')
    $excludePath = Join-Path $ignoredRoot '.git\info\exclude'
    $originalExclude = [IO.File]::ReadAllText($excludePath)
    Write-UTF8WithoutBOM -Path $excludePath -Value ($originalExclude + "`n/$goProbeRelative`n")
    Write-UTF8WithoutBOM -Path $goProbePath -Value @'
package buildinfo

import "os"

func init() {
	if os.Getenv("TANNANG_RC_IGNORED_PROBE") == "panic" {
		panic("ignored RC provenance probe")
	}
}
'@

    Push-Location $ignoredRoot
    try {
        $listedGoFiles = @(& go list -f '{{.GoFiles}}' ./internal/buildinfo 2>&1)
        $goListExit = $LASTEXITCODE
        $dependencies = @(& go list -deps ./cmd/tannang 2>&1)
        $depsExit = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
    if ($goListExit -ne 0 -or ($listedGoFiles -join '') -notmatch 'zz_rc_ignored_probe\.go') {
        throw "Ignored Go probe is not an actual buildinfo package input."
    }
    if ($depsExit -ne 0 -or $dependencies -notcontains 'github.com/05wuyanzi/tannang/internal/buildinfo') {
        throw "The buildinfo package is not reachable from ./cmd/tannang."
    }
    Assert-ExactIgnoredFile -RepositoryRoot $ignoredRoot -RelativePath $goProbeRelative

    $goRejectOutput = Join-Path $tempRoot 'ignored-go-output'
    $ignoredHelper = Join-Path $ignoredRoot 'scripts\release\build-windows-amd64.ps1'
    $goReject = Invoke-BuildHelper -HelperPath $ignoredHelper -OutputDirectory $goRejectOutput -Mode RC -LogPath (Join-Path $tempRoot 'ignored-go.log')
    if ($goReject.ExitCode -eq 0) {
        throw "RC helper accepted an ignored untracked Go build input."
    }
    if (Test-Path -LiteralPath $goRejectOutput) {
        throw "RC helper published an output root before rejecting ignored Go input."
    }

    Remove-Item -LiteralPath $goProbePath -Force
    Write-UTF8WithoutBOM -Path $excludePath -Value $originalExclude

    $nonGoRelative = 'zz_rc_ignored_probe.tmp'
    $nonGoPath = Join-Path $ignoredRoot $nonGoRelative
    Write-UTF8WithoutBOM -Path $nonGoPath -Value "ignored non-Go provenance probe`n"
    Write-UTF8WithoutBOM -Path $excludePath -Value ($originalExclude + "`n/$nonGoRelative`n")
    Assert-ExactIgnoredFile -RepositoryRoot $ignoredRoot -RelativePath $nonGoRelative

    $nonGoRejectOutput = Join-Path $tempRoot 'ignored-nongo-output'
    $nonGoReject = Invoke-BuildHelper -HelperPath $ignoredHelper -OutputDirectory $nonGoRejectOutput -Mode RC -LogPath (Join-Path $tempRoot 'ignored-nongo.log')
    if ($nonGoReject.ExitCode -eq 0) {
        throw "RC helper accepted an ignored untracked non-Go file."
    }
    if (Test-Path -LiteralPath $nonGoRejectOutput) {
        throw "RC helper published an output root before rejecting ignored non-Go input."
    }
    Remove-Item -LiteralPath $nonGoPath -Force
    Write-UTF8WithoutBOM -Path $excludePath -Value $originalExclude

    $containedRoot = Join-Path $tempRoot 'contained-output'
    New-IsolatedRepository -SourceRepository $fixtureRoot -DestinationRepository $containedRoot
    $containedStateBefore = Get-RepositoryState -RepositoryRoot $containedRoot
    $containedHelper = Join-Path $containedRoot 'scripts\release\build-windows-amd64.ps1'
    $equalReject = Invoke-BuildHelper -HelperPath $containedHelper -OutputDirectory $containedRoot.ToUpperInvariant() -Mode RC -LogPath (Join-Path $tempRoot 'repo-equal.log')
    if ($equalReject.ExitCode -eq 0 -or $equalReject.Output -notmatch 'outside the repository root') {
        throw "RC helper did not reject OutputDirectory equal to the repository root through the containment gate."
    }
    $containedOutput = (Join-Path $containedRoot 'portable-output-inside-source').ToUpperInvariant()
    $containedReject = Assert-RCBuildRejected -RepositoryRoot $containedRoot -OutputDirectory $containedOutput -LogPath (Join-Path $tempRoot 'repo-contained.log')
    if ($containedReject.Output -notmatch 'outside the repository root') {
        throw "RC helper did not reject nested OutputDirectory through the containment gate."
    }
    $containedStateAfter = Get-RepositoryState -RepositoryRoot $containedRoot
    Assert-RepositoryStateEqual -Expected $containedStateBefore -Actual $containedStateAfter -Context 'Repository-contained output rejection'

    $cleanStatus = Invoke-GitCapture -RepositoryRoot $fixtureRoot -GitArguments @('status', '--porcelain=v1', '--untracked-files=all')
    $cleanIgnored = Invoke-GitCapture -RepositoryRoot $fixtureRoot -GitArguments @('ls-files', '--others', '--ignored', '--exclude-standard')
    if ($cleanStatus.Length -ne 0 -or $cleanIgnored.Length -ne 0) {
        throw "Temporary fixture did not return to a clean RC boundary."
    }

    $cleanOutput = Join-Path $tempRoot 'fixture-output'
    $cleanBuild = Invoke-BuildHelper -HelperPath $fixtureHelper -OutputDirectory $cleanOutput -Mode RC -LogPath (Join-Path $tempRoot 'clean-rc.log')
    if ($cleanBuild.ExitCode -ne 0) {
        throw "Clean temporary RC build failed: $($cleanBuild.Output)"
    }
    $expectedFiles = @('BUILD-INFO.json', 'LICENSE', 'SHA256SUMS.txt', 'tannang.exe')
    $actualFiles = @(Get-ChildItem -LiteralPath $cleanOutput -File | ForEach-Object { $_.Name } | Sort-Object)
    if (@(Compare-Object $expectedFiles $actualFiles).Count -ne 0) {
        throw "Clean temporary RC build produced an unexpected file set."
    }
    $buildInfo = Get-Content -Raw -LiteralPath (Join-Path $cleanOutput 'BUILD-INFO.json') | ConvertFrom-Json
    $expectedProductVersion = "0.0.0-pre-alpha+git.$fixtureHead"
    if ($buildInfo.build_mode -ne 'RC' -or $buildInfo.vcs -ne 'git' -or $buildInfo.source_revision -cne $fixtureHead -or
        [bool]$buildInfo.source_modified -or $buildInfo.goos -ne 'windows' -or $buildInfo.goarch -ne 'amd64' -or
        $buildInfo.product_version -cne $expectedProductVersion) {
        throw "Clean temporary RC build identity is inconsistent."
    }
    Assert-ManifestValid -ArtifactRoot $cleanOutput
    Assert-WindowsAMD64PE -BinaryPath (Join-Path $cleanOutput 'tannang.exe')
    if ((Get-FileHash -LiteralPath (Join-Path $cleanOutput 'LICENSE') -Algorithm SHA256).Hash -cne
        (Get-FileHash -LiteralPath (Join-Path $fixtureRoot 'LICENSE') -Algorithm SHA256).Hash) {
        throw "Clean temporary RC build LICENSE differs from repository LICENSE."
    }

    $sourceStateAfter = Get-RepositoryState -RepositoryRoot $sourceRoot
    Assert-RepositoryStateEqual -Expected $sourceStateBefore -Actual $sourceStateAfter -Context 'Regression suite'

    $result = [ordered]@{
        test_temp_repo_external                    = $true
        active_powershell_executable               = $script:activePowerShellExecutable
        active_powershell_executable_exists        = $true
        current_candidate_helper_bytes_used        = $true
        development_tracked_modified               = $true
        development_staged_modified                = $true
        development_untracked_modified             = $true
        rc_tracked_rejected                        = $true
        rc_staged_rejected                         = $true
        rc_untracked_rejected                      = $true
        ignored_go_probe                           = $goProbeRelative
        git_status_hides_ignored_go_probe           = $true
        git_ls_files_detects_ignored_go_probe       = $true
        ignored_go_probe_is_build_input             = $true
        rc_ignored_go_build_input_rejected          = $true
        output_root_absent_after_go_rejection       = $true
        rc_ignored_nongo_file_rejected              = $true
        output_root_absent_after_nongo_rejection    = $true
        repository_root_output_rejected             = $true
        repository_contained_output_rejected        = $true
        repository_contained_rejected_before_write  = $true
        sibling_prefix_output_allowed               = $true
        clean_temp_boundary_pass                    = $true
        clean_temp_rc_build_revision                = $fixtureHead
        clean_temp_rc_build_source_modified         = [bool]$buildInfo.source_modified
        clean_temp_rc_product_version               = [string]$buildInfo.product_version
        manifest_independently_recalculated         = $true
        windows_pe_amd64_verified                   = $true
        license_bytes_match                         = $true
        real_repo_git_state_mutated                 = $false
        real_collection_executed                    = $false
        network_used_for_dependencies               = $false
    }
}
finally {
    if (Test-Path -LiteralPath $tempRoot) {
        $resolvedTempRoot = [IO.Path]::GetFullPath($tempRoot)
        if (-not $resolvedTempRoot.StartsWith($tempPrefix, [StringComparison]::OrdinalIgnoreCase) -or -not (Split-Path -Leaf $resolvedTempRoot).StartsWith('tannang-rc-ignored-test-', [StringComparison]::Ordinal)) {
            throw "Refusing to clean an unsafe regression-test path."
        }
        Remove-Item -LiteralPath $resolvedTempRoot -Recurse -Force
    }
}

if ($null -eq $result) {
    throw "Regression test completed without a result."
}
$result | ConvertTo-Json -Depth 3
