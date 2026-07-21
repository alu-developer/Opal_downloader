# Keeps ~/.claude/rate-limit-status.json fresh by running a hidden, idle
# `claude` process whose status line writes it.
#
# WHY: the status line is the only writer of that file, and it does not run in
# non-interactive or claude-desktop sessions - so the file goes stale exactly
# when an autonomous run needs it. On 2026-07-21 it was 18 hours old and still
# reporting "1%".
#
# This implementation follows the research in ~/.claude/skills/queue-run
# (SKILL.md, "keep-warm"), which established the non-obvious parts:
#
#  - `--dangerously-skip-permissions` is required, not for permissions, but
#    because it skips the startup trust dialog that otherwise blocks a hidden
#    launch from ever syncing. Safe here specifically because nothing sends
#    this process a prompt, so no tool call is ever attempted. It leans on an
#    undocumented side effect - re-verify if keep-warm silently stops working
#    after a CLI update.
#  - A hidden *window* is required. Redirecting stdout/stderr strips the
#    pseudo-console and `claude` falls back to --print mode, which exits
#    immediately.
#  - The process must be RECYCLED (~hourly). It syncs from the server on
#    startup; the idle refresh only re-stamps cached numbers, so a process
#    left running forever goes stale forever once its window rolls over.
#  - A live process is not proof of a fresh file. Only a write that postdates
#    the launch AND carries a non-null five_hour counts, because the status
#    line writes a {"five_hour": null} placeholder on a fresh session before
#    its first real round-trip.
#
# Verified live on this machine 2026-07-21: a cold launch produced a real sync
# in 35s, taking the file from 18h stale ("1%") to five_hour 46% / seven_day 48%.
#
# Windows-only. Exits 0 always; a failure here must never block anything.

$ErrorActionPreference = 'SilentlyContinue'

$pidFile = Join-Path $env:USERPROFILE ".claude\queue-run-keepwarm.pid"
$statusFile = Join-Path $env:USERPROFILE ".claude\rate-limit-status.json"

# Well under the 5-hour window, so one stale sync cannot survive across it.
$maxAgeSeconds = 3600

$alive = $false
if (Test-Path $pidFile) {
    $existingPid = Get-Content $pidFile -ErrorAction SilentlyContinue
    if ($existingPid) {
        $proc = Get-Process -Id $existingPid -ErrorAction SilentlyContinue
        # ProcessName check is a cheap guard against a recycled PID now
        # belonging to something unrelated.
        if ($proc -and $proc.ProcessName -eq 'powershell') {
            if (((Get-Date) - $proc.StartTime).TotalSeconds -lt $maxAgeSeconds) {
                $alive = $true
            } else {
                Stop-Process -Id $existingPid -Force -ErrorAction SilentlyContinue
            }
        }
    }
}

if ($alive) {
    Write-Output "keepwarm: reusing existing process"
    exit 0
}

$launchedAt = Get-Date
try {
    $p = Start-Process powershell `
        -ArgumentList '-NoExit', '-Command', 'claude --dangerously-skip-permissions' `
        -WindowStyle Hidden -PassThru -ErrorAction Stop
    Set-Content -Path $pidFile -Value $p.Id -ErrorAction SilentlyContinue
} catch {
    Write-Output "keepwarm: launch failed: $($_.Exception.Message)"
    exit 0
}

# Wait for proof of a real sync. Only paid when a process was actually just
# started; a reused one returned above.
for ($i = 0; $i -lt 6; $i++) {
    Start-Sleep -Seconds 7
    if (Test-Path $statusFile) {
        $written = (Get-Item $statusFile).LastWriteTime
        if ($written -gt $launchedAt -and
            (Get-Content $statusFile -Raw) -notmatch '"five_hour":\s*null') {
            Write-Output "keepwarm: synced after $(($i + 1) * 7)s"
            exit 0
        }
    }
}

Write-Output "keepwarm: started but no sync confirmed within 42s"
exit 0
