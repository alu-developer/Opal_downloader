param(
    [Parameter(Position = 0)]
    [ValidateSet("deps", "fmt", "vet", "test", "build", "all", "playwright", "race")]
    [string]$Task = "all"
)

$ErrorActionPreference = "Stop"
$go = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $go)) {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $cmd) {
        throw "Go executable not found. Install Go or add it to PATH."
    }
    $go = $cmd.Source
}

Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    switch ($Task) {
        "deps" {
            & $go mod tidy
        }
        "fmt" {
            & $go fmt ./...
        }
        "vet" {
            & $go vet ./...
        }
        "test" {
            & $go test ./...
        }
        "build" {
            & $go build ./...
        }
        "playwright" {
            & $go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0 install
        }
        "race" {
            # Deliberately NOT part of "all": the race detector requires
            # CGO_ENABLED=1 plus a C toolchain, which a stock Windows box does
            # not have. Rather than letting "all" fail with Go's fairly opaque
            # `C compiler "gcc" not found`, this target checks first and says
            # what to do about it. CI runs -race on every PR regardless (see
            # .github/workflows/ci.yml), so a missing local toolchain costs
            # coverage on this machine, not on the project.
            $cc = Get-Command gcc -ErrorAction SilentlyContinue
            if ($null -eq $cc) {
                Write-Warning "The race detector needs a C toolchain and none was found on PATH."
                Write-Host "Install one (e.g. 'winget install -e --id MSYS2.MSYS2' then 'pacman -S mingw-w64-x86_64-gcc',"
                Write-Host "or 'choco install mingw'), reopen the shell, and re-run: scripts/dev.ps1 race"
                Write-Host ""
                Write-Host "Until then, -race still runs on every pull request in CI."
                exit 1
            }
            $env:CGO_ENABLED = "1"
            & $go test -race ./...
        }
        "all" {
            & $go mod tidy
            & $go fmt ./...
            & $go vet ./...
            & $go test ./...
            & $go build ./...

            # Cross-platform vet. Everything above only ever builds for the
            # host, so a symbol behind //go:build windows referenced from an
            # untagged file (or test) looks perfectly fine here and only
            # explodes on CI's Linux runner. That is not hypothetical: it
            # happened on 2026-07-22 (PR #122), where "all" passed locally and
            # CI failed with `undefined: withExecutableState`.
            #
            # vet, not test: the tests cannot run under a foreign GOOS anyway,
            # and vet already compiles each package, which is what catches this
            # class of break.
            Write-Host "vet (GOOS=linux)..."
            $previousGOOS = $env:GOOS
            try {
                $env:GOOS = "linux"
                & $go vet ./...
            }
            finally {
                $env:GOOS = $previousGOOS
            }
        }
    }
}
finally {
    Pop-Location
}
