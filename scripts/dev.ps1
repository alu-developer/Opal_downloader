param(
    [Parameter(Position = 0)]
    [ValidateSet("deps", "fmt", "vet", "test", "build", "all", "playwright", "race", "hooks")]
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
        "hooks" {
            & (Join-Path $PSScriptRoot "test-hooks.ps1")
            if ($LASTEXITCODE -ne 0) { throw "hook tests failed" }
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

            # The .claude/hooks/ safety net. Not Go, but it is the machinery
            # that decides when autonomous runs continue and what survives a
            # turn killed mid-run - a silent break there is invisible until an
            # actual incident, which is how it broke on 2026-07-23. Runs last
            # because it is the cheapest step to re-run in isolation.
            Write-Host "hook tests..."
            & (Join-Path $PSScriptRoot "test-hooks.ps1")
            if ($LASTEXITCODE -ne 0) { throw "hook tests failed" }

            # tmp/ is gitignored scratch space for manual probe/debug runs and
            # has no automated writer of its own - unlike .claude/queue/ and
            # refs/wip-checkpoints/ (see docs/BACKLOG.md's "Noticed" section,
            # 2026-07-30), which are pruned on every invocation of the
            # scheduled process that writes them, nothing ever ran on a
            # schedule here, so nothing ever pruned it either. Same fix, same
            # shape: age-based, and "all" is the closest thing tmp/ has to a
            # recurring invocation, since pre-push-gate.ps1 runs it before
            # every push. No count floor is needed the way the other two
            # entries have one: those get written automatically and often
            # enough that a pure age cutoff could plausibly race a burst of
            # writes; tmp/ is written by hand, sporadically, so 14 days is
            # just distance from "clearly abandoned", not a safety rail.
            $tmpDir = Join-Path (Split-Path $PSScriptRoot -Parent) "tmp"
            if (Test-Path $tmpDir) {
                $cutoff = (Get-Date).AddDays(-14)
                $stale = @(Get-ChildItem $tmpDir -File -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt $cutoff })
                if ($stale.Count -gt 0) {
                    Write-Host "pruning $($stale.Count) stale file(s) from tmp/ (older than 14 days)..."
                    $stale | Remove-Item -Force
                }
            }
        }
    }
}
finally {
    Pop-Location
}
