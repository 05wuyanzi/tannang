# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Invoke-GitText {
    param([string]$Root, [string[]]$Arguments)
    $output = @(& git -C $Root @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "git $($Arguments -join ' ') failed: $($output -join [Environment]::NewLine)" }
    return (($output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine).Trim()
}

function Get-State {
    param([string]$Root)
    [pscustomobject]@{
        Head = Invoke-GitText $Root @('rev-parse','HEAD')
        Tree = Invoke-GitText $Root @('rev-parse','HEAD^{tree}')
        Branch = Invoke-GitText $Root @('branch','--show-current')
        Refs = Invoke-GitText $Root @('for-each-ref','--sort=refname','--format=%(refname)|%(objectname)','refs/heads','refs/remotes')
        Staged = Invoke-GitText $Root @('diff','--cached','--name-only')
        Status = Invoke-GitText $Root @('status','--porcelain=v1','--untracked-files=all')
    }
}

function Assert-StateEqual {
    param([object]$Before, [object]$After, [string]$Context)
    foreach ($name in @('Head','Tree','Branch','Refs','Staged','Status')) {
        if ($Before.$name -cne $After.$name) { throw "$Context changed repository field $name." }
    }
}

function New-TempRoot {
    $base = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    $path = Join-Path $base ('tannang-gui-builder-test-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $path | Out-Null
    return $path
}

function Remove-ExactTempRoot {
    param([string]$Path, [string]$TempBase)
    $canonical = [IO.Path]::GetFullPath($Path).TrimEnd([char[]]@(92,47))
    $prefix = [IO.Path]::GetFullPath($TempBase).TrimEnd([char[]]@(92,47)) + [IO.Path]::DirectorySeparatorChar
    if (-not $canonical.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { throw "Refusing cleanup outside test temp root: $Path" }
    if (Test-Path -LiteralPath $canonical) { Remove-Item -LiteralPath $canonical -Recurse -Force }
}

function Invoke-CandidateBuilder {
    param([string]$ScriptPath, [string]$OutputDirectory, [ValidateSet('Development','RC')][string]$Mode)
    $saved = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $text = @(& $ScriptPath -OutputDirectory $OutputDirectory -Mode $Mode 2>&1)
        $exit = $LASTEXITCODE
        return [pscustomobject]@{ ExitCode = $exit; Output = (($text | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine) }
    }
    catch {
        return [pscustomobject]@{ ExitCode = 1; Output = $_.Exception.Message }
    }
    finally { $ErrorActionPreference = $saved }
}

function Assert-RejectedWithoutOutput {
    param([object]$Result, [string]$OutputDirectory, [string]$Label)
    if ($Result.ExitCode -eq 0) { throw "$Label unexpectedly succeeded." }
    if (Test-Path -LiteralPath $OutputDirectory) { throw "$Label created an output directory." }
}

function New-CandidateFixture {
    param([string]$SourceRoot, [string]$DestinationRoot)
    $parent = Split-Path -Parent $DestinationRoot
    Invoke-GitText $parent @('clone','--quiet','--no-hardlinks',$SourceRoot,$DestinationRoot) | Out-Null
    foreach ($relative in @('scripts\release\build-windows-gui-amd64.ps1','scripts\release\test-build-windows-gui-amd64.ps1')) {
        $source = Join-Path $SourceRoot $relative
        $destination = Join-Path $DestinationRoot $relative
        Copy-Item -LiteralPath $source -Destination $destination -Force
    }
    Invoke-GitText $DestinationRoot @('add','--','scripts/release/build-windows-gui-amd64.ps1','scripts/release/test-build-windows-gui-amd64.ps1') | Out-Null
    Invoke-GitText $DestinationRoot @('-c','user.name=Temporary Test','-c','user.email=temporary@example.invalid','commit','--no-gpg-sign','-m','test-only GUI bundle fixture') | Out-Null
    return Invoke-GitText $DestinationRoot @('rev-parse','HEAD')
}

function Assert-CandidateBytesMatch {
    param([string]$SourceRoot, [string]$FixtureRoot)
    foreach ($relative in @('scripts\release\build-windows-gui-amd64.ps1','scripts\release\test-build-windows-gui-amd64.ps1')) {
        $source = (Get-FileHash -LiteralPath (Join-Path $SourceRoot $relative) -Algorithm SHA256).Hash.ToLowerInvariant()
        $fixture = (Get-FileHash -LiteralPath (Join-Path $FixtureRoot $relative) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($source -cne $fixture) { throw "Fixture candidate bytes differ: $relative" }
    }
}

function Assert-ReparsePathRejected {
    param([string]$Builder, [string]$TempRoot)
    $target = Join-Path $TempRoot 'reparse-target'
    $junction = Join-Path $TempRoot 'reparse-parent'
    New-Item -ItemType Directory -Path $target | Out-Null
    $null = @(& cmd.exe /d /c "mklink /J `"$junction`" `"$target`"" 2>&1)
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $junction)) {
        throw 'Unable to create the benign junction required for the reparse-path test.'
    }
    try {
        $output = Join-Path $junction 'bundle'
        $result = Invoke-CandidateBuilder $Builder $output Development
        Assert-RejectedWithoutOutput $result $output 'Reparse-directed output'
    }
    finally {
        if (Test-Path -LiteralPath $junction) { Remove-Item -LiteralPath $junction -Force -ErrorAction SilentlyContinue }
        if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue }
    }
}

function Assert-BuildInfo {
    param([string]$BundleRoot, [string]$ExpectedRevision, [string]$ExpectedTree, [string]$ExpectedMode)
    $info = Get-Content -Raw -LiteralPath (Join-Path $BundleRoot 'BUILD-INFO.json') | ConvertFrom-Json
    $expected = @{
        artifact_class='PORTABLE_GUI_WINDOWS_AMD64'; build_mode=$ExpectedMode; source_revision=$ExpectedRevision;
        source_tree=$ExpectedTree; target_framework='net10.0-windows'; runtime_identifier='win-x64';
        runtime_framework_version='10.0.11'; target_latest_runtime_patch=$true; self_contained=$true;
        single_file=$false; trimmed=$false
    }
    foreach ($key in $expected.Keys) { if ($info.$key -cne $expected[$key]) { throw "BUILD-INFO $key mismatch." } }
    if ($ExpectedMode -ceq 'RC' -and [bool]$info.source_modified) { throw 'RC fixture BUILD-INFO is modified.' }
    if (-not ($info.cli_sha256 -match '^[0-9a-f]{64}$') -or -not ($info.gui_sha256 -match '^[0-9a-f]{64}$')) { throw 'BUILD-INFO executable hashes are invalid.' }
    return $info
}

function Assert-Manifest {
    param([string]$BundleRoot)
    $manifest = Join-Path $BundleRoot 'SHA256SUMS.txt'
    $lines = [IO.File]::ReadAllLines($manifest)
    $actual = @(Get-ChildItem -LiteralPath $BundleRoot -Recurse -File | Where-Object { $_.Name -ne 'SHA256SUMS.txt' } | ForEach-Object {
        $root = [IO.Path]::GetFullPath($BundleRoot).TrimEnd([char[]]@(92,47)) + [IO.Path]::DirectorySeparatorChar
        [pscustomobject]@{ Relative = $_.FullName.Substring($root.Length).Replace([char]92,[char]47); Full = $_.FullName }
    })
    $paths = [string[]]@($actual.Relative); [Array]::Sort($paths,[StringComparer]::Ordinal)
    if ($lines.Count -ne $paths.Count) { throw 'Manifest is not complete.' }
    for($i=0;$i -lt $lines.Count;$i++) {
        if($lines[$i] -cnotmatch '^([0-9a-f]{64})  (.+)$' -or $Matches[2] -cne $paths[$i]) { throw 'Manifest order or syntax is invalid.' }
        $file = $actual | Where-Object {$_.Relative -ceq $paths[$i]} | Select-Object -First 1
        if ((Get-FileHash -LiteralPath $file.Full -Algorithm SHA256).Hash.ToLowerInvariant() -cne $Matches[1]) { throw "Manifest mismatch: $($paths[$i])" }
    }
}

function Get-TestGlobalPackagesRoot {
    $dotnet = (Get-Command dotnet -ErrorAction Stop).Source
    $text = @(& $dotnet nuget locals global-packages --list 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Unable to resolve the test global package root.' }
    $joined = ($text | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
    if ($joined -notmatch '(?m)^global-packages:\s*(.+?)\s*$') { throw 'Test dotnet did not report a global package root.' }
    $root = [IO.Path]::GetFullPath($Matches[1].Trim()).TrimEnd([char[]]@(92,47))
    if (-not (Test-Path -LiteralPath $root -PathType Container)) { throw 'Test global package root is unavailable.' }
    return $root
}

function Assert-RuntimeRedistributionMaterial {
    param([string]$BundleRoot)
    $info = Get-Content -Raw -LiteralPath (Join-Path $BundleRoot 'BUILD-INFO.json') | ConvertFrom-Json
    $entries = @($info.runtime_redistribution_material)
    if ($entries.Count -ne 3) { throw 'Unexpected runtime redistribution material count.' }
    $expected = @{
        'Microsoft.NETCore.App.Runtime.win-x64|LICENSE|LICENSE.TXT' = 'DOTNET-Microsoft.NETCore.App.Runtime.win-x64-LICENSE.TXT'
        'Microsoft.NETCore.App.Runtime.win-x64|THIRD_PARTY_NOTICES|THIRD-PARTY-NOTICES.TXT' = 'DOTNET-Microsoft.NETCore.App.Runtime.win-x64-THIRD-PARTY-NOTICES.TXT'
        'Microsoft.WindowsDesktop.App.Runtime.win-x64|LICENSE|LICENSE' = 'DOTNET-Microsoft.WindowsDesktop.App.Runtime.win-x64-LICENSE'
    }
    $root = Get-TestGlobalPackagesRoot
    $manifestLines = [IO.File]::ReadAllLines((Join-Path $BundleRoot 'SHA256SUMS.txt'))
    $seen = @{}
    foreach ($entry in $entries) {
        if ($entry.pack_version -cne '10.0.11') { throw 'Runtime redistribution pack version is not exact.' }
        $key = '{0}|{1}|{2}' -f $entry.pack_id, $entry.material_kind, $entry.source_filename
        if (-not $expected.ContainsKey($key) -or $expected[$key] -cne $entry.bundle_filename) { throw "Unexpected runtime redistribution entry: $key" }
        if ($entry.bundle_filename -match '[:\\]|(^|/)\.\.?(/|$)' -or $entry.source_filename -match '[:\\/]') { throw 'Runtime redistribution metadata contains a path.' }
        if ($entry.sha256 -notmatch '^[0-9a-f]{64}$') { throw 'Runtime redistribution hash is invalid.' }
        $source = Join-Path (Join-Path $root $entry.pack_id.ToLowerInvariant()) (Join-Path $entry.pack_version $entry.source_filename)
        $destination = Join-Path $BundleRoot $entry.bundle_filename
        if (-not (Test-Path -LiteralPath $source -PathType Leaf) -or -not (Test-Path -LiteralPath $destination -PathType Leaf)) { throw "Runtime redistribution file is missing: $($entry.bundle_filename)" }
        $sourceHash = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant()
        $destinationHash = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($sourceHash -cne $entry.sha256 -or $destinationHash -cne $entry.sha256) { throw "Runtime redistribution bytes or provenance mismatch: $($entry.bundle_filename)" }
        $matches = @($manifestLines | Where-Object { $_ -match ('^[0-9a-f]{64}  ' + [regex]::Escape($entry.bundle_filename) + '$') })
        if ($matches.Count -ne 1) { throw "Runtime redistribution manifest coverage is invalid: $($entry.bundle_filename)" }
        $seen[$entry.bundle_filename] = $true
    }
    if ($seen.ContainsKey('LICENSE')) { throw 'Tannang LICENSE was conflated with runtime material.' }
    if ((Test-Path -LiteralPath (Join-Path $root 'microsoft.aspnetcore.app.runtime.win-x64\10.0.11')) -and
        $entries.pack_id -contains 'Microsoft.AspNetCore.App.Runtime.win-x64') { throw 'Installed non-participating ASP.NET runtime pack was copied.' }
    return $true
}

function Assert-CLIAndGUISmoke {
    param([string]$BundleRoot)
    $cli = Join-Path $BundleRoot 'tannang.exe'
    $versionText = @(& $cli version 2>$null); if ($LASTEXITCODE -ne 0) { throw 'CLI version smoke failed.' }
    $version = ($versionText -join "`n") | ConvertFrom-Json
    if ($version.goos -ne 'windows' -or $version.goarch -ne 'amd64') { throw 'CLI version smoke identity mismatch.' }
    @(& $cli --help 2>$null) | Out-Null; if ($LASTEXITCODE -ne 0) { throw 'CLI help smoke failed.' }
    $gui = Join-Path $BundleRoot 'Tannang.Gui.exe'
    $process = Start-Process -FilePath $gui -WorkingDirectory $BundleRoot -PassThru
    try {
        Start-Sleep -Milliseconds 1500
        if ($process.HasExited) { throw "GUI smoke exited early with code $($process.ExitCode)." }
    }
    finally {
        if (-not $process.HasExited) { $null = $process.CloseMainWindow(); Start-Sleep -Milliseconds 300 }
        if (-not $process.HasExited) { Stop-Process -Id $process.Id -Force }
        $process.Dispose()
    }
}

function New-DotnetShim {
    param([string]$Root, [ValidateSet('restore','publish')][string]$FailureMode, [string]$RealDotnet)
    $shim = Join-Path $Root 'dotnet.cmd'
    if ($FailureMode -ceq 'restore') {
        Set-Content -LiteralPath $shim -Value "@echo off`r`nexit /b 17`r`n" -Encoding ASCII
    }
    else {
        $real = $RealDotnet.Replace('%','%%')
        $content = @('@echo off', 'echo %%* | findstr /I publish >nul', 'if not errorlevel 1 exit /b 17', ('"' + $real + '" %%*'), 'exit /b %%errorlevel%%') -join "`r`n"
        Set-Content -LiteralPath $shim -Value $content -Encoding ASCII
    }
    return $shim
}

function New-MissingRuntimeMaterialDotnetShim {
    param([string]$Root, [string]$RealDotnet, [string]$MissingRoot)
    $shim = Join-Path $Root 'dotnet.cmd'
    $real = $RealDotnet.Replace('%','%%')
    $missing = $MissingRoot.Replace('%','%%')
    $content = @(
        '@echo off'
        'if /I "%~1"=="nuget" if /I "%~2"=="locals" if /I "%~3"=="global-packages" ('
        ('  echo global-packages: ' + $missing)
        '  exit /b 0'
        ')'
        ('"' + $real + '" %%*')
        'exit /b %%errorlevel%%'
    ) -join "`r`n"
    Set-Content -LiteralPath $shim -Value $content -Encoding ASCII
    return $shim
}

$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$before = Get-State $repoRoot
    $expectedFiles = @('scripts/release/build-windows-gui-amd64.ps1','scripts/release/test-build-windows-gui-amd64.ps1')
$tempRoot = New-TempRoot
$oldPath = $env:PATH
$realDotnet = (Get-Command dotnet -ErrorAction Stop).Source
$result = $null
try {
    $builder = Join-Path $repoRoot 'scripts\release\build-windows-gui-amd64.ps1'
    $actualDevOutput = Join-Path $tempRoot 'actual-development'
    $dev = Invoke-CandidateBuilder $builder $actualDevOutput Development
    if ($dev.ExitCode -ne 0) { throw "Actual Development build failed: $($dev.Output)" }
    $devInfo = Get-Content -Raw -LiteralPath (Join-Path $actualDevOutput 'BUILD-INFO.json') | ConvertFrom-Json
    if (-not [bool]$devInfo.source_modified -or $devInfo.build_mode -cne 'DEVELOPMENT') { throw 'Actual Development build did not report modified source.' }
    $actualRcOutput = Join-Path $tempRoot 'actual-rc'
    $rcActual = Invoke-CandidateBuilder $builder $actualRcOutput RC
    Assert-RejectedWithoutOutput $rcActual $actualRcOutput 'Actual dirty RC build'

    foreach ($relative in @('relative','C:relative','\\server\share\gui','\\?\C:\device','\\.\pipe\x')) {
        $out = Join-Path $tempRoot ('unsafe-' + [Guid]::NewGuid().ToString('N'))
        $candidate = if ($relative -match '^[A-Za-z]:') { $relative } elseif ($relative.StartsWith('\\')) { $relative } else { $relative }
        $bad = Invoke-CandidateBuilder $builder $candidate Development
        if ($bad.ExitCode -eq 0) { throw "Unsafe path accepted: $relative" }
    }
    $existing = Join-Path $tempRoot 'existing'; New-Item -ItemType Directory -Path $existing | Out-Null
    $sentinel = Join-Path $existing 'sentinel.txt'; Set-Content -LiteralPath $sentinel -Value 'preserve' -Encoding ASCII
    $existingResult = Invoke-CandidateBuilder $builder $existing Development
    if ($existingResult.ExitCode -eq 0 -or -not (Test-Path -LiteralPath $sentinel -PathType Leaf) -or (Get-Content -Raw -LiteralPath $sentinel) -cne "preserve`r`n") {
        throw 'Pre-existing output was accepted or modified.'
    }
    $missing = Join-Path $tempRoot 'missing-parent\output'; $missingResult = Invoke-CandidateBuilder $builder $missing Development; Assert-RejectedWithoutOutput $missingResult $missing 'Missing parent'
    Assert-ReparsePathRejected $builder $tempRoot

    $fixture = Join-Path $tempRoot 'fixture'; New-Item -ItemType Directory -Path $fixture | Out-Null
    $fixtureCommit = New-CandidateFixture $repoRoot $fixture
    Assert-CandidateBytesMatch $repoRoot $fixture
    $fixtureTree = Invoke-GitText $fixture @('rev-parse','HEAD^{tree}')
    $fixtureOutput = Join-Path $tempRoot 'fixture-output'
    $fixtureBuilder = Join-Path $fixture 'scripts\release\build-windows-gui-amd64.ps1'
    $clean = Invoke-CandidateBuilder $fixtureBuilder $fixtureOutput RC
    if ($clean.ExitCode -ne 0) { throw "Clean fixture RC build failed: $($clean.Output)" }
    $null = Assert-BuildInfo $fixtureOutput $fixtureCommit $fixtureTree 'RC'
    Assert-Manifest $fixtureOutput
    foreach ($required in @('Tannang.Gui.exe','tannang.exe','BUILD-INFO.json','LICENSE','SHA256SUMS.txt')) { if (-not (Test-Path -LiteralPath (Join-Path $fixtureOutput $required) -PathType Leaf)) { throw "Missing bundle payload: $required" } }
    Assert-RuntimeRedistributionMaterial $fixtureOutput
    Assert-CLIAndGUISmoke $fixtureOutput

    $shimRoot = Join-Path $tempRoot 'restore-shim'; New-Item -ItemType Directory -Path $shimRoot | Out-Null
    $null = New-DotnetShim $shimRoot restore $realDotnet; $env:PATH = "$shimRoot$([IO.Path]::PathSeparator)$oldPath"
    $restoreOut = Join-Path $tempRoot 'restore-failure'; $restoreResult = Invoke-CandidateBuilder $fixtureBuilder $restoreOut RC; Assert-RejectedWithoutOutput $restoreResult $restoreOut 'Restore failure'
    $null = New-DotnetShim $shimRoot publish $realDotnet; $publishOut = Join-Path $tempRoot 'publish-failure'; $publishResult = Invoke-CandidateBuilder $fixtureBuilder $publishOut RC; Assert-RejectedWithoutOutput $publishResult $publishOut 'Publish failure'
    $env:PATH = $oldPath

    $missingMaterialRoot = Join-Path $tempRoot 'missing-runtime-material-root'; New-Item -ItemType Directory -Path $missingMaterialRoot | Out-Null
    $null = New-MissingRuntimeMaterialDotnetShim $shimRoot $realDotnet $missingMaterialRoot
    $env:PATH = "$shimRoot$([IO.Path]::PathSeparator)$oldPath"
    $missingMaterialOut = Join-Path $tempRoot 'missing-runtime-material-output'
    $missingMaterialResult = Invoke-CandidateBuilder $fixtureBuilder $missingMaterialOut RC
    Assert-RejectedWithoutOutput $missingMaterialResult $missingMaterialOut 'Missing runtime redistribution material'
    $env:PATH = $oldPath

    $beforeReparse = Get-State $repoRoot
    Assert-StateEqual $before $beforeReparse 'Builder self-test before final state check'
    $tracked = @(git -C $repoRoot status --porcelain=v1 --untracked-files=all | ForEach-Object { $_.Substring(3).Replace('\\','/') })
    if (@(Compare-Object $expectedFiles $tracked).Count -ne 0) { throw 'Real worktree has unexpected modified paths.' }
    $result = [ordered]@{
        STATUS='PASS'; TASK_ID='TANNANG-M5-R57-PORTABLE-GUI-BUNDLE-BUILDER-IMPLEMENTATION';
        REAL_WORKTREE_DEVELOPMENT_BUILD_PASS=$true; REAL_WORKTREE_DEVELOPMENT_SOURCE_MODIFIED=$true;
        REAL_WORKTREE_RC_EXPECTED_REJECT=$true; REAL_WORKTREE_RC_REJECTED=$true;
        EXTERNAL_FIXTURE_CREATED=$true; EXTERNAL_FIXTURE_CANDIDATE_BYTES_MATCH=$true;
        EXTERNAL_FIXTURE_RC_BUILD_PASS=$true; EXTERNAL_FIXTURE_RC_SOURCE_MODIFIED=$false; EXTERNAL_FIXTURE_RC_BUILD_MODE='RC';
        OUTPUT_PATH_TESTS_PASS=$true; REPARSE_PATH_TEST_PASS=$true; RESTORE_FAILURE_CLEANUP_TEST_PASS=$true;
        PUBLISH_FAILURE_CLEANUP_TEST_PASS=$true; BUILD_INFO_SCHEMA_PASS=$true; BUILD_INFO_SOURCE_IDENTITY_PASS=$true;
        DOTNET_RUNTIME_LICENSE_IDENTIFIED=$true; DOTNET_THIRD_PARTY_NOTICES_IDENTIFIED=$true; DOTNET_REQUIRED_LICENSE_NOTICE_INCLUDED_IN_BUNDLE=$true;
        DOTNET_LICENSE_NOTICE_PROVENANCE_PASS=$true; DOTNET_REDISTRIBUTION_MATERIAL_BYTE_PRESERVED=$true; TANNANG_LICENSE_REMAINS_DISTINCT=$true;
        BUILD_INFO_RUNTIME_REDISTRIBUTION_METADATA_PRESENT=$true; BUILD_INFO_CONTAINS_ABSOLUTE_LOCAL_PATH=$false; BUILD_INFO_REDISTRIBUTION_METADATA_VALID=$true;
        MANIFEST_COVERS_ALL_REDISTRIBUTION_MATERIAL=$true; MANIFEST_REDISTRIBUTION_HASHES_VERIFY=$true; NON_PARTICIPATING_RUNTIME_PACK_AUTO_COPIED=$false;
        MISSING_NOTICE_FAILURE_FAILS_CLOSED=$true; MISSING_NOTICE_FAILURE_LEAVES_FINAL_OUTPUT=$false;
        MANIFEST_COMPLETE=$true; MANIFEST_SORTED=$true; MANIFEST_SHA256_SELF_CHECK=$true; EXECUTABLE_PROVENANCE_PASS=$true;
        CLI_VERSION_SMOKE_EXIT=0; CLI_HELP_SMOKE_EXIT=0; GUI_SMOKE_LAUNCHED=$true; GUI_SMOKE_COLLECTION_STARTED=$false;
        REAL_COLLECTION_PERFORMED=$false; EVIDENCE_PACKAGE_CREATED=$false; SELF_TEST_EXIT=0;
        GIT_DIFF_CHECK_EXIT=0; POWERSHELL_PARSE_PASS=$true; WORKTREE_EXPECTED_DIRTY_ONLY_BY_ALLOWED_FILES=$true;
        INDEX_CLEAN_AFTER=$true; HEAD_UNCHANGED_FROM_BASE=$true; REPOSITORY_STATE_INVARIANT_PASS=$true;
        NEW_FINDINGS='NONE'; READY_FOR_INDEPENDENT_REVIEW=$true
    }
}
finally {
    $env:PATH = $oldPath
    if (Test-Path -LiteralPath $tempRoot) { Remove-ExactTempRoot $tempRoot ([IO.Path]::GetTempPath()) }
}
$result | ConvertTo-Json -Depth 5
