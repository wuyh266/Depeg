$ErrorActionPreference = 'Stop'
$projectPath = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$envPath = Join-Path $projectPath '.env'
if (Test-Path -LiteralPath $envPath) {
    Write-Host '.env already exists; it was not changed.'
    exit 0
}
$randomBytes = New-Object byte[] 32
$randomSource = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $randomSource.GetBytes($randomBytes) } finally { $randomSource.Dispose() }
$signingKey = [Convert]::ToBase64String($randomBytes)
$content = [System.IO.File]::ReadAllText((Join-Path $projectPath '.env.example'))
$content = $content.Replace('JWT_SIGNING_KEY=', ('JWT_SIGNING_KEY=' + $signingKey))
[System.IO.File]::WriteAllText($envPath, $content, (New-Object System.Text.UTF8Encoding($false)))
Write-Host 'Created .env with a random JWT signing key. Fill MYSQL_DSN locally; no database password was generated or printed.'
