param([string]$CCompiler = "gcc")
$ErrorActionPreference = "Stop"
$env:CGO_ENABLED = "1"
$env:CC = $CCompiler
if ((go env GOOS) -ne "windows" -or (go env GOARCH) -ne "amd64") {
    throw "Run this build on Windows amd64."
}
go build -trimpath -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin
if ($LASTEXITCODE -ne 0) { throw "Plugin build failed." }
