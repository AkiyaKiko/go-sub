$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$output = Join-Path $root "gosub.exe"
$gocache = Join-Path $root ".gocache"

if (-not (Test-Path $gocache)) {
  New-Item -ItemType Directory -Path $gocache | Out-Null
}
$env:GOCACHE = $gocache

Write-Host "Generating templ views..."
go run github.com/a-h/templ/cmd/templ generate
if ($LASTEXITCODE -ne 0) {
  throw "templ generate failed."
}

Write-Host "Building Go executable..."
go build -trimpath -ldflags "-s -w" -o $output .
if ($LASTEXITCODE -ne 0) {
  throw "Go build failed."
}

Write-Host "Done: $output"
