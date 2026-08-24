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
    param([string]$RepositoryRoot, [string[]]$GitArguments)
    $result = @(& git -C $RepositoryRoot @GitArguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "git $($GitArguments -join ' ') failed: $($result -join [Environment]::NewLine)"
    }
    return (($result | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine).Trim()
}

function Invoke-Tool {
    param([string]$Command, [string[]]$Arguments, [string]$Context)
    $output = @(& $Command @Arguments 2>&1)
    $exitCode = $LASTEXITCODE
    $text = ($output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
    if ($exitCode -ne 0) {
        throw "$Context failed (exit $exitCode): $text"
    }
    return $text
}

function Get-CanonicalPath {
    param([string]$Path)
    $full = [IO.Path]::GetFullPath($Path)
    $root = [IO.Path]::GetPathRoot($full)
    if ([string]::Equals($full, $root, [StringComparison]::OrdinalIgnoreCase)) { return $root }
    return $full.TrimEnd([char[]]@([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar))
}

function Assert-SafeOutputPath {
    param([string]$Path, [string]$RepositoryRoot)
    if ([string]::IsNullOrWhiteSpace($Path) -or $Path -match '^[A-Za-z]:[^\\/]' -or
        $Path -match '^(\\\\|//|\\\\\?\\|//\?/|\\\\\.\\|//\./|\\\?\?|/dev/|/proc/|/sys/)') {
        throw 'OutputDirectory must be an ordinary absolute local Windows path.'
    }
    if (-not [IO.Path]::IsPathRooted($Path)) { throw 'OutputDirectory must be absolute.' }
    $output = Get-CanonicalPath $Path
    $repo = Get-CanonicalPath $RepositoryRoot
    $repoPrefix = $repo.TrimEnd([char[]]@(92,47)) + [IO.Path]::DirectorySeparatorChar
    if ([string]::Equals($output, $repo, [StringComparison]::OrdinalIgnoreCase) -or
        $output.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'OutputDirectory must be outside the repository root.'
    }
    if (Test-Path -LiteralPath $output) { throw 'OutputDirectory already exists.' }
    $parent = Split-Path -Parent $output
    if (-not (Test-Path -LiteralPath $parent -PathType Container)) { throw 'OutputDirectory parent must already exist.' }
    $item = Get-Item -LiteralPath $parent -Force
    while ($null -ne $item) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Output path traverses a reparse point.' }
        $item = $item.Parent
    }
    try {
        $drive = [IO.DriveInfo]::new([IO.Path]::GetPathRoot($output)).DriveType
        if ($drive -ne [IO.DriveType]::Fixed -and $drive -ne [IO.DriveType]::Removable) {
            throw "Output drive must be Fixed or Removable; observed $drive."
        }
    }
    catch [System.Management.Automation.MethodException] { throw 'Unable to classify output drive.' }
    return $output
}

function Get-ActivePowerShell {
    $path = [Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
    if ([string]::IsNullOrWhiteSpace($path) -or -not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw 'Unable to resolve the active PowerShell executable.'
    }
    return $path
}

function Copy-CurrentCandidateScripts {
    param([string]$SourceRoot, [string]$DestinationRoot)
    foreach ($relative in @('scripts\release\build-windows-gui-amd64.ps1', 'scripts\release\test-build-windows-gui-amd64.ps1')) {
        $source = Join-Path $SourceRoot $relative
        if (Test-Path -LiteralPath $source -PathType Leaf) {
            $destination = Join-Path $DestinationRoot $relative
            $parent = Split-Path -Parent $destination
            if (-not (Test-Path -LiteralPath $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
            Copy-Item -LiteralPath $source -Destination $destination -Force
        }
    }
}

function New-ExternalRepository {
    param([string]$SourceRoot, [string]$DestinationRoot)
    $parent = Split-Path -Parent $DestinationRoot
    Invoke-Tool 'git' @('-C', $parent, 'clone', '--quiet', '--no-hardlinks', $SourceRoot, $DestinationRoot) 'external repository clone' | Out-Null
    Copy-CurrentCandidateScripts -SourceRoot $SourceRoot -DestinationRoot $DestinationRoot
}

function Set-OfflineDotnetEnvironment {
    $env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'
    $env:DOTNET_SKIP_FIRST_TIME_EXPERIENCE = '1'
    $env:DOTNET_NOLOGO = '1'
    $env:DOTNET_CLI_WORKLOAD_UPDATE_NOTIFY_DISABLE = '1'
}

function Write-OfflineNuGetConfig {
    param([string]$Path)
    $xml = @'
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
  </packageSources>
</configuration>
'@
    [IO.File]::WriteAllText($Path, $xml, (New-Object Text.UTF8Encoding($false)))
}

function Get-DotnetGlobalPackagesRoot {
    param([string]$Dotnet)
    $text = Invoke-Tool $Dotnet @('nuget','locals','global-packages','--list') 'resolve global package root'
    if ($text -notmatch '(?m)^global-packages:\s*(.+?)\s*$') { throw 'dotnet did not report a global package root.' }
    $root = Get-CanonicalPath $Matches[1].Trim()
    if (-not (Test-Path -LiteralPath $root -PathType Container)) { throw 'dotnet global package root is unavailable.' }
    $item = Get-Item -LiteralPath $root -Force
    while ($null -ne $item) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'dotnet global package root traverses a reparse point.' }
        $item = $item.Parent
    }
    return $root
}

function Assert-SourceFile {
    param([string]$Path, [string]$Context)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "$Context is missing." }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "$Context is a reparse point." }
    while ($null -ne $item) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "$Context traverses a reparse point." }
        $item = $item.Parent
    }
}

function Get-ParticipatingRuntimePacks {
    param([string]$DepsPath)
    if (-not (Test-Path -LiteralPath $DepsPath -PathType Leaf)) { throw 'GUI publish dependency metadata is missing.' }
    $deps = Get-Content -Raw -LiteralPath $DepsPath | ConvertFrom-Json
    $packs = @{}
    foreach ($target in $deps.targets.psobject.Properties) {
        foreach ($library in $target.Value.psobject.Properties.Name) {
            if ($library -match '^runtimepack\.(?<id>[^/]+)/(?<version>[^/]+)$') {
                $id = $Matches.id
                $version = $Matches.version
                if ($id -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or $version -cne '10.0.11') {
                    throw "Unsupported participating runtime pack identity: $library"
                }
                $packs[$id] = [pscustomobject]@{ PackId = $id; Version = $version }
            }
        }
    }
    if ($packs.Count -eq 0) { throw 'GUI publish did not identify any participating runtime packs.' }
    return @($packs.Values | Sort-Object PackId)
}

function Copy-AndVerifyRuntimeMaterial {
    param([string]$DepsPath, [string]$GlobalPackagesRoot, [string]$BundleRoot)
    $entries = [System.Collections.Generic.List[object]]::new()
    foreach ($pack in @(Get-ParticipatingRuntimePacks $DepsPath)) {
        $packRoot = Join-Path (Join-Path $GlobalPackagesRoot $pack.PackId.ToLowerInvariant()) $pack.Version
        if (-not (Test-Path -LiteralPath $packRoot -PathType Container)) { throw "Participating runtime pack root is missing: $($pack.PackId)/$($pack.Version)" }
        $packItem = Get-Item -LiteralPath $packRoot -Force
        while ($null -ne $packItem) {
            if (($packItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Participating runtime pack root traverses a reparse point: $($pack.PackId)" }
            $packItem = $packItem.Parent
        }
        $sources = @(Get-ChildItem -LiteralPath $packRoot -File | Where-Object {
            $_.Name -match '^(LICENSE(?:\.[^.]+)?|THIRD[-_]PARTY[-_]NOTICES(?:\.[^.]+)?)$'
        } | Sort-Object Name)
        if ($sources.Count -eq 0) { throw "No runtime license or notice material found for $($pack.PackId)/$($pack.Version)." }
        foreach ($source in $sources) {
            Assert-SourceFile $source.FullName "runtime redistribution material $($source.FullName)"
            $kind = if ($source.Name -match '^LICENSE') { 'LICENSE' } else { 'THIRD_PARTY_NOTICES' }
            $bundleName = 'DOTNET-{0}-{1}' -f $pack.PackId, $source.Name
            $destination = Join-Path $BundleRoot $bundleName
            if (Test-Path -LiteralPath $destination) { throw "Runtime redistribution material name collision: $bundleName" }
            $sourceHash = (Get-FileHash -LiteralPath $source.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            Copy-Item -LiteralPath $source.FullName -Destination $destination
            $destinationHash = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($sourceHash -cne $destinationHash) { throw "Runtime redistribution material copy changed bytes: $bundleName" }
            $entries.Add([ordered]@{
                pack_id = $pack.PackId
                pack_version = $pack.Version
                material_kind = $kind
                source_filename = $source.Name
                bundle_filename = $bundleName
                sha256 = $sourceHash
            })
        }
    }
    return @($entries | Sort-Object pack_id, material_kind, source_filename)
}

function Invoke-HeadlessBuilder {
    param([string]$RepositoryRoot, [string]$OutputRoot)
    $helper = Join-Path $RepositoryRoot 'scripts\release\build-windows-amd64.ps1'
    if (-not (Test-Path -LiteralPath $helper -PathType Leaf)) { throw 'Headless builder is missing from the staging repository.' }
    $powershell = Get-ActivePowerShell
    Invoke-Tool $powershell @('-NoProfile','-ExecutionPolicy','Bypass','-File',$helper,'-OutputDirectory',$OutputRoot,'-Mode',$Mode) 'headless CLI build' | Out-Null
    $info = Get-Content -Raw -LiteralPath (Join-Path $OutputRoot 'BUILD-INFO.json') | ConvertFrom-Json
    if ($info.artifact_class -cne 'HEADLESS_PORTABLE_WINDOWS_AMD64') { throw 'Headless builder returned an unexpected artifact class.' }
    if (-not (Test-Path -LiteralPath (Join-Path $OutputRoot 'tannang.exe') -PathType Leaf)) { throw 'Headless builder did not produce tannang.exe.' }
    return $info
}

function Invoke-DotnetPublish {
    param([string]$RepositoryRoot, [string]$WorkingRoot, [string]$PublishRoot)
    $dotnet = (Get-Command dotnet -ErrorAction Stop).Source
    $globalPackagesRoot = Get-DotnetGlobalPackagesRoot $dotnet
    Set-OfflineDotnetEnvironment
    $nuget = Join-Path $WorkingRoot 'NuGet.Config'
    Write-OfflineNuGetConfig $nuget
    $project = Join-Path $RepositoryRoot 'gui\Tannang.Gui\Tannang.Gui.csproj'
    $msbuild = Join-Path $WorkingRoot 'msbuild'
    $obj = Join-Path $msbuild 'obj'
    $bin = Join-Path $msbuild 'bin'
    $common = @(
        '-p:RuntimeIdentifier=win-x64', '-p:RuntimeFrameworkVersion=10.0.11',
        '-p:TargetLatestRuntimePatch=true', '-p:SelfContained=true',
        '-p:PublishSingleFile=false', '-p:PublishTrimmed=false',
        ('-p:BaseIntermediateOutputPath=' + ($obj + '\')),
        ('-p:MSBuildProjectExtensionsPath=' + ($obj + '\')),
        ('-p:BaseOutputPath=' + ($bin + '\')),
        '-p:RestoreIgnoreFailedSources=false'
    )
    Invoke-Tool $dotnet (@('restore',$project,'--configfile',$nuget,'--nologo') + $common) 'offline GUI restore' | Out-Null
    $repoBin = Join-Path $RepositoryRoot 'gui\Tannang.Gui\bin'
    $repoObj = Join-Path $RepositoryRoot 'gui\Tannang.Gui\obj'
    if ((Test-Path -LiteralPath $repoBin) -or (Test-Path -LiteralPath $repoObj)) {
        throw 'GUI restore created repository-local bin/obj residue.'
    }
    Invoke-Tool $dotnet (@('publish',$project,'--configuration','Release','--no-restore','--nologo','--output',$PublishRoot) + $common) 'offline GUI publish' | Out-Null
    if (-not (Test-Path -LiteralPath (Join-Path $PublishRoot 'Tannang.Gui.exe') -PathType Leaf)) { throw 'GUI publish did not produce Tannang.Gui.exe.' }
    return [pscustomobject]@{
        DepsPath = Join-Path $PublishRoot 'Tannang.Gui.deps.json'
        GlobalPackagesRoot = $globalPackagesRoot
    }
}

function Get-RelativeFiles {
    param([string]$Root)
    $rootCanonical = (Get-CanonicalPath $Root).TrimEnd([char[]]@(92,47)) + [IO.Path]::DirectorySeparatorChar
    return @(Get-ChildItem -LiteralPath $Root -Recurse -File | ForEach-Object {
        $relative = $_.FullName.Substring($rootCanonical.Length).Replace([char]92,[char]47)
        if ([string]::IsNullOrWhiteSpace($relative) -or $relative -match '(^|/)\.\.?(/|$)' -or $relative.StartsWith('/')) { throw "Invalid bundle relative path: $relative" }
        [pscustomobject]@{ Relative = $relative; Full = $_.FullName }
    })
}

function Write-BuildInfo {
    param([string]$Path, [object]$CliInfo, [string]$SourceTree, [string]$CliHash, [string]$GuiHash, [object[]]$RuntimeRedistributionMaterial)
    $info = [ordered]@{
        artifact_class = 'PORTABLE_GUI_WINDOWS_AMD64'
        build_mode = [string]$CliInfo.build_mode
        source_revision = [string]$CliInfo.source_revision
        source_tree = $SourceTree
        source_modified = [bool]$CliInfo.source_modified
        cli_sha256 = $CliHash
        gui_sha256 = $GuiHash
        target_framework = 'net10.0-windows'
        runtime_identifier = 'win-x64'
        runtime_framework_version = '10.0.11'
        target_latest_runtime_patch = $true
        self_contained = $true
        single_file = $false
        trimmed = $false
        runtime_redistribution_material = @($RuntimeRedistributionMaterial)
    }
    if ($Mode -eq 'RC' -and $info.source_modified) { throw 'RC GUI bundle reports modified source.' }
    [IO.File]::WriteAllText($Path, (($info | ConvertTo-Json -Depth 4) + "`n"), (New-Object Text.UTF8Encoding($false)))
    return $info
}

function Write-AndVerifyManifest {
    param([string]$BundleRoot)
    $manifest = Join-Path $BundleRoot 'SHA256SUMS.txt'
    $files = @(Get-RelativeFiles $BundleRoot | Where-Object { $_.Relative -cne 'SHA256SUMS.txt' })
    $paths = [string[]]@($files.Relative)
    [Array]::Sort($paths, [StringComparer]::Ordinal)
    $lines = foreach ($relative in $paths) {
        $file = $files | Where-Object { $_.Relative -ceq $relative } | Select-Object -First 1
        "$( (Get-FileHash -LiteralPath $file.Full -Algorithm SHA256).Hash.ToLowerInvariant() )  $relative"
    }
    [IO.File]::WriteAllText($manifest, (($lines -join "`n") + "`n"), [Text.Encoding]::ASCII)
    $actual = @(Get-RelativeFiles $BundleRoot | Where-Object { $_.Relative -cne 'SHA256SUMS.txt' })
    $actualPaths = [string[]]@($actual.Relative); [Array]::Sort($actualPaths, [StringComparer]::Ordinal)
    if ($actualPaths.Count -ne $paths.Count -or (Compare-Object $paths $actualPaths).Count -ne 0) { throw 'Bundle manifest file set is incomplete.' }
    $manifestLines = [IO.File]::ReadAllLines($manifest)
    if ($manifestLines.Count -ne $paths.Count) { throw 'Bundle manifest entry count is invalid.' }
    for ($i = 0; $i -lt $manifestLines.Count; $i++) {
        if ($manifestLines[$i] -cnotmatch '^([0-9a-f]{64})  (.+)$' -or $Matches[2] -cne $paths[$i]) { throw 'Bundle manifest ordering or syntax is invalid.' }
        $full = Join-Path $BundleRoot ($paths[$i].Replace('/', '\'))
        if (-not (Test-Path -LiteralPath $full -PathType Leaf) -or (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant() -cne $Matches[1]) { throw "Bundle manifest mismatch: $($paths[$i])" }
    }
}

$repoRoot = Get-CanonicalPath (Join-Path $PSScriptRoot '..\..')
$reportedRoot = Get-CanonicalPath (Invoke-GitCapture $repoRoot @('rev-parse','--show-toplevel'))
if (-not [string]::Equals($repoRoot, $reportedRoot, [StringComparison]::OrdinalIgnoreCase)) { throw 'Builder repository identity mismatch.' }
$outputRoot = Assert-SafeOutputPath $OutputDirectory $repoRoot
$tempRoot = Join-Path (Split-Path -Parent $outputRoot) ('.tannang-gui-build-' + [Guid]::NewGuid().ToString('N'))
$sourceDirty = $false
try {
    $head = Invoke-GitCapture $repoRoot @('rev-parse','HEAD')
    $tree = Invoke-GitCapture $repoRoot @('rev-parse','HEAD^{tree}')
    $status = Invoke-GitCapture $repoRoot @('status','--porcelain=v1','--untracked-files=all')
    $sourceDirty = $status.Length -ne 0
    & git -C $repoRoot diff --cached --quiet --exit-code; $stagedExit = $LASTEXITCODE
    if ($stagedExit -gt 1) { throw 'Unable to determine index state.' }
    if ($Mode -eq 'RC') {
        $ignored = Invoke-GitCapture $repoRoot @('ls-files','--others','--ignored','--exclude-standard')
        if ($sourceDirty -or $stagedExit -ne 0 -or $ignored.Length -ne 0) { throw 'RC mode requires clean tracked, staged, untracked, and ignored state.' }
    }
    New-Item -ItemType Directory -Path $tempRoot | Out-Null
    $stageRepo = Join-Path $tempRoot 'repo'
    $headlessOut = Join-Path $tempRoot 'headless'
    $publishOut = Join-Path $tempRoot 'gui-publish'
    $bundle = Join-Path $tempRoot 'bundle'
    New-ExternalRepository $repoRoot $stageRepo
    $cliInfo = Invoke-HeadlessBuilder $stageRepo $headlessOut
    $cliHash = (Get-FileHash -LiteralPath (Join-Path $headlessOut 'tannang.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
    $publishDetails = Invoke-DotnetPublish $stageRepo $tempRoot $publishOut
    Copy-Item -LiteralPath $publishOut -Destination $bundle -Recurse
    Copy-Item -LiteralPath (Join-Path $headlessOut 'tannang.exe') -Destination (Join-Path $bundle 'tannang.exe')
    $runtimeMaterial = Copy-AndVerifyRuntimeMaterial $publishDetails.DepsPath $publishDetails.GlobalPackagesRoot $bundle
    $guiHash = (Get-FileHash -LiteralPath (Join-Path $bundle 'Tannang.Gui.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
    $null = Write-BuildInfo (Join-Path $bundle 'BUILD-INFO.json') $cliInfo $tree $cliHash $guiHash $runtimeMaterial
    Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE') -Destination (Join-Path $bundle 'LICENSE')
    Write-AndVerifyManifest $bundle
    $publishedExe = @(Get-ChildItem -LiteralPath $publishOut -Recurse -File -Filter '*.exe' | ForEach-Object { (Get-CanonicalPath $_.FullName).Substring((Get-CanonicalPath $publishOut).Length).TrimStart([char[]]@(92,47)) })
    $bundleExe = @(Get-ChildItem -LiteralPath $bundle -Recurse -File -Filter '*.exe' | ForEach-Object { (Get-CanonicalPath $_.FullName).Substring((Get-CanonicalPath $bundle).Length).TrimStart([char[]]@(92,47)) })
    foreach ($exe in $bundleExe) {
        if ($exe -cne 'tannang.exe' -and $publishedExe -notcontains $exe) { throw "Unexpected executable outside controlled publish: $exe" }
    }
    $expectedFiles = @('BUILD-INFO.json','LICENSE','SHA256SUMS.txt','Tannang.Gui.exe','tannang.exe') + @($runtimeMaterial | ForEach-Object bundle_filename)
    foreach ($required in $expectedFiles) { if (-not (Test-Path -LiteralPath (Join-Path $bundle $required) -PathType Leaf)) { throw "Missing required bundle file: $required" } }
    Move-Item -LiteralPath $bundle -Destination $outputRoot
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
    $finalInfo = Get-Content -Raw -LiteralPath (Join-Path $outputRoot 'BUILD-INFO.json') | ConvertFrom-Json
    $finalInfo | ConvertTo-Json -Depth 4
}
catch {
    if (Test-Path -LiteralPath $tempRoot) { Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue }
    if (Test-Path -LiteralPath $outputRoot) { Remove-Item -LiteralPath $outputRoot -Recurse -Force -ErrorAction SilentlyContinue }
    throw
}
