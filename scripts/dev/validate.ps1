# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$goFiles = @(Get-ChildItem -LiteralPath $PSScriptRoot\..\.. -Recurse -File -Filter '*.go')
$unformatted = @(
    if ($goFiles.Count -gt 0) {
        gofmt -l -- $goFiles.FullName
    }
)
if ($unformatted.Count -gt 0) {
    throw "Go files require formatting: $($unformatted -join ', ')"
}

go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go test ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go test -race ./...
exit $LASTEXITCODE
