# Cross-compile linux/amd64 and build .deb + .apk packages.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path (Join-Path $root "go.mod"))) {
    $root = Get-Location | Select-Object -ExpandProperty Path
}
Set-Location $root

$version = (Get-Content -Raw (Join-Path $root "packaging\VERSION")).Trim()
$out = Join-Path $root "dist\linux-amd64"
$pkg = Join-Path $root "dist\packages"
New-Item -ItemType Directory -Force -Path $out, $pkg | Out-Null

$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$ldflags = "-s -w"

Write-Host "building binaries $version linux/amd64"
go build -ldflags $ldflags -o (Join-Path $out "seccopy-client") ./cmd/client
go build -ldflags $ldflags -o (Join-Path $out "seccopy-server") ./cmd/server
go build -ldflags $ldflags -o (Join-Path $out "seccopy-publish") ./cmd/publish

if (-not (Get-Command nfpm -ErrorAction SilentlyContinue)) {
    Write-Host "installing nfpm"
    go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.4
}

$nfpm = Get-Command nfpm -ErrorAction SilentlyContinue
if (-not $nfpm) {
    $nfpmBin = Join-Path (go env GOPATH) "bin\nfpm.exe"
} else {
    $nfpmBin = $nfpm.Source
}

$configs = @(
    "packaging/nfpm-client.yaml",
    "packaging/nfpm-server.yaml",
    "packaging/nfpm-publish.yaml",
    "packaging/nfpm-meta.yaml"
)
foreach ($cfg in $configs) {
    foreach ($packager in @("deb", "apk")) {
        Write-Host "nfpm $packager $cfg"
        & $nfpmBin pkg --config $cfg --packager $packager --target $pkg
    }
}

Write-Host "packages in $pkg"
Get-ChildItem $pkg | Select-Object Name, Length
