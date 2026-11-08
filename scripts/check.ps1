$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath ([System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')))
& go build -buildvcs=false ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& go test -buildvcs=false ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& go vet -buildvcs=false ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& go mod verify
exit $LASTEXITCODE
