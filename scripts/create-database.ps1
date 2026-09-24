# =============================================================================
# create-database.ps1 — idempotently create the Staff Display databases
# =============================================================================
# Usage:
#   pwsh ./scripts/create-database.ps1
#   pwsh ./scripts/create-database.ps1 -Password 'secret' -HostName '127.0.0.1' -SkipMigrate
#
# Reads the PostgreSQL superuser password from, in order:
#   1. -Password parameter
#   2. PGPASSWORD environment variable
#   3. backend/.env  (DATABASE_PASSWORD)
# =============================================================================
[CmdletBinding()]
param(
    [string]$HostName    = "127.0.0.1",
    [int]   $Port        = 5432,
    [string]$User        = "postgres",
    [string]$Password    = "",
    [string[]]$Databases = @("staffdisplay", "staffdisplay_test"),
    [string]$PsqlPath    = "C:\Program Files\PostgreSQL\16\bin\psql.exe",
    [switch]$SkipMigrate
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot

if (-not $Password) { $Password = $env:PGPASSWORD }
if (-not $Password) {
    $envFile = Join-Path $repoRoot "backend/.env"
    if (Test-Path $envFile) {
        $match = Select-String -Path $envFile -Pattern '^\s*DATABASE_PASSWORD\s*=\s*(.+)$'
        if ($match) { $Password = $match.Matches[0].Groups[1].Value.Trim() }
    }
}
if (-not $Password) {
    throw "PostgreSQL password not provided. Use -Password, PGPASSWORD, or backend/.env (DATABASE_PASSWORD)."
}
if (-not (Test-Path $PsqlPath)) {
    $candidate = Get-Command psql.exe -ErrorAction SilentlyContinue
    if (-not $candidate) { throw "psql.exe not found at '$PsqlPath' and not on PATH." }
    $PsqlPath = $candidate.Source
}

$env:PGPASSWORD = $Password

function Invoke-Psql {
    param([string]$Database, [string]$Command)
    $output = & $PsqlPath -U $User -h $HostName -p $Port -d $Database -v ON_ERROR_STOP=1 -t -A -c $Command 2>&1
    if ($LASTEXITCODE -ne 0) { throw "psql failed ($Database): $output" }
    return $output
}

Write-Host "Checking PostgreSQL $HostName`:$Port as '$User' ..."
$null = Invoke-Psql -Database "postgres" -Command "select 1;"
Write-Host "  connection OK"

foreach ($db in $Databases) {
    $exists = Invoke-Psql -Database "postgres" -Command "select 1 from pg_database where datname = '$db';"
    if ($exists -eq "1") {
        Write-Host "  database '$db' already exists"
    } else {
        $null = Invoke-Psql -Database "postgres" -Command "create database $db;"
        Write-Host "  database '$db' created"
    }
}

if ($SkipMigrate) {
    Write-Host "Skipping migrations (-SkipMigrate)."
    exit 0
}

Write-Host "Applying migrations to 'staffdisplay' ..."
Push-Location (Join-Path $repoRoot "backend")
try {
    & go run ./cmd/migrate up
    if ($LASTEXITCODE -ne 0) { throw "migration failed (exit $LASTEXITCODE)" }
    & go run ./cmd/migrate status
    if ($LASTEXITCODE -ne 0) { throw "migration status failed (exit $LASTEXITCODE)" }
} finally {
    Pop-Location
}

Write-Host "Done."
