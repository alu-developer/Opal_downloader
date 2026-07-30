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

# NOT 'Stop': Write-Error under ErrorActionPreference='Stop' raises a
# terminating error, which aborts the script with exit code 1 before it can
# reach `exit 2`. Claude Code only treats exit 2 as blocking, so the gate
# would have printed a scary message and then let the push through anyway -
# found by testing the block path rather than assuming it. Failures are
# written straight to stderr and paired with an explicit `exit 2` instead.
$ErrorActionPreference = 'Continue'

function Deny($message) {
    [Console]::Error.WriteLine($message)
    exit 2
}

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
# push. Covered by the matcher tests in scripts/test-hooks.ps1 - it fails safe
# (runs dev.ps1 needlessly) rather than unsafe, but a gate that cries wolf gets
# ignored.
$scan = $command -replace '"[^"]*"', '' -replace "'[^']*'", ''

# Match a real push: `git push`, `git -C ... push`, `git push --force`.
#
# `\r\n` is in the excluded set for a reason found the hard way on 2026-07-30.
# A negated character class matches newlines, so `[^;&|]*` spanned them, and a
# heredoc commit
#
#     git commit -F - <<'EOF'
#     ... then push again
#     EOF
#
# glued the body's "push" to the `git` on line 1 and gated a plain commit as a
# push. Quote-stripping does not help: a heredoc body is not quoted. It cost
# two blocked commits before anyone read the regex, since the gate's message
# says the build failed rather than that it decided this was a push.
if ($scan -notmatch '(^|[\s;&|(])git\b[^;&|\r\n]*\bpush\b') { exit 0 }

# A dry run changes nothing on the remote, so gating it is pure cost.
if ($scan -match '--dry-run') { exit 0 }

Write-Host "pre-push gate: running scripts/dev.ps1 all before pushing..."

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$devScript = Join-Path $repoRoot "scripts\dev.ps1"
# Test seam, same shape as the other hooks' OPAL_* overrides. Without it the
# suite could only ever exercise the "not a push" branch, because the real
# branch shells out to the very script that runs the suite. This is a local
# convenience gate, not a security boundary - anyone able to set env vars for
# this process can already run git directly.
if ($env:OPAL_PREPUSH_DEV_SCRIPT) { $devScript = $env:OPAL_PREPUSH_DEV_SCRIPT }
if (-not (Test-Path $devScript)) {
    Deny "pre-push gate: $devScript not found - refusing to push unverified."
}

$code = 1
$threw = $null
Push-Location $repoRoot
try {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $devScript all
    $code = $LASTEXITCODE
} catch {
    $threw = $_
} finally {
    Pop-Location
}

# Denying happens after the location is restored, because Deny exits the
# process and would otherwise strand the caller in the repo root.
if ($threw) {
    Deny "pre-push gate: dev.ps1 threw: $threw"
}
if ($code -ne 0) {
    # "Fix it" alone sent a reader hunting for a nonexistent bug twice on
    # 2026-07-30. The most common cause is ambient, not broken code: the hook
    # suite asserts that nothing else is running in this tree, so a live probe
    # run (TestFileListSnapshot and friends, several minutes each) fails it
    # legitimately. Say so here rather than in a doc nobody reads mid-block.
    Deny @"
pre-push gate: scripts/dev.ps1 all failed (exit $code) - push blocked.

Check the output above before assuming the code is broken. If a live test or
probe run is active in this tree, the hook suite's "nothing else in this repo
is running" precondition fails by design - wait for it to finish and retry.
Otherwise fix the failure and push again.
"@
}

Write-Host "pre-push gate: dev.ps1 all passed."
exit 0
