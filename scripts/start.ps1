$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath ([System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')))
& go run -buildvcs=false ./cmd/api
exit $LASTEXITCODE
