$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$webDir = Join-Path $root "web"
$output = Join-Path $root "gosub.exe"
$gocache = Join-Path $root ".gocache"

if (-not (Test-Path $gocache)) {
  New-Item -ItemType Directory -Path $gocache | Out-Null
}
$env:GOCACHE = $gocache

Write-Host "[1/2] Building web frontend..."
Push-Location $webDir
pnpm build
if ($LASTEXITCODE -ne 0) {
  Pop-Location
  throw "Frontend build failed."
}
Pop-Location

Write-Host "[2/2] Building Go executable..."
go build -trimpath -ldflags "-s -w" -o $output .
if ($LASTEXITCODE -ne 0) {
  throw "Go build failed."
}

Write-Host "Done: $output"
