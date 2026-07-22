# PreToolUse hook: run scripts/dev.ps1 all before any command that pushes.
#
# Why this is a script rather than settings.json's `if:` matcher.
# The gate was originally declared as `"if": "Bash(git push*)"`, which only
# matches a command that *begins* with `git push`. Every push actually issued
# in this repo looks like
#
#     cd "C:\...\Opal_downloader" && git push -u origin some-branch
#
# so the gate never fired once - for months, silently. Broadening it to
# `Bash(*git push*)` did not help either (verified 2026-07-22: a compound push
# still completed in ~2s, far too fast for dev.ps1). Rather than keep guessing
# at the matcher's pattern semantics, the hook now receives the tool input and
# decides for itself. That is testable in isolation:
#
#     '{"tool_input":{"command":"cd /x && git push"}}' | powershell -File .claude/hooks/pre-push-gate.ps1
#
# BLOCKS ON FAILURE (exit 2), unlike the autopilot gate's fail-open design.
# The whole point of a pre-push check is to stop broken code reaching the
# remote, so an inconclusive result must not be treated as a pass. It only
# fails open when it cannot tell whether this is a push at all (unreadable
# input), which would otherwise break every unrelated Bash call.

$ErrorActionPreference = 'Stop'

try {
    $raw = [Console]::In.ReadToEnd()
} catch {
    exit 0
}
if (-not $raw) { exit 0 }

try {
    $payload = $raw | ConvertFrom-Json
} catch {
    exit 0
}

# Both the Bash and PowerShell tools carry the command here.
$command = ''
if ($payload.tool_input) {
    if ($payload.tool_input.command) { $command = [string]$payload.tool_input.command }
}
if (-not $command) { exit 0 }

# Strip quoted strings first, so a commit message that happens to contain the
# word "push" (`git commit -m "push the parser harder"`) is not mistaken for a
# push. Caught by the matcher tests below - it fails safe (runs dev.ps1
# needlessly) rather than unsafe, but a gate that cries wolf gets ignored.
$scan = $command -replace '"[^"]*"', '' -replace "'[^']*'", ''

# Match a real push: `git push`, `git -C ... push`, `git push --force`.
if ($scan -notmatch '(^|[\s;&|(])git\b[^;&|]*\bpush\b') { exit 0 }

# A dry run changes nothing on the remote, so gating it is pure cost.
if ($scan -match '--dry-run') { exit 0 }

Write-Host "pre-push gate: running scripts/dev.ps1 all before pushing..."

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$devScript = Join-Path $repoRoot "scripts\dev.ps1"
if (-not (Test-Path $devScript)) {
    Write-Error "pre-push gate: $devScript not found - refusing to push unverified."
    exit 2
}

Push-Location $repoRoot
try {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $devScript all
    $code = $LASTEXITCODE
} catch {
    Write-Error "pre-push gate: dev.ps1 threw: $_"
    exit 2
} finally {
    Pop-Location
}

if ($code -ne 0) {
    Write-Error "pre-push gate: scripts/dev.ps1 all failed (exit $code) - push blocked. Fix it, then push again."
    exit 2
}

Write-Host "pre-push gate: dev.ps1 all passed."
exit 0
