# SessionStart hook: arms autopilot automatically instead of requiring the
# maintainer to run the PowerShell snippet from docs/agent-operating-model.md
# by hand at the start of every session.
#
# WHY: the autopilot Stop hook (autopilot-gate.ps1) only engages when
# .claude/queue/AUTOPILOT exists, and that file was previously created
# manually. In practice it was rarely armed, so autopilot rarely ran even for
# sessions opened correctly in this directory. This closes that gap for the
# one case a hook CAN close: a session whose settings.json is actually
# loaded, i.e. one started in this directory. It cannot help a session opened
# elsewhere and pointed here by path - project settings are fixed to the
# directory the session started in, with no later re-scoping. See
# docs/agent-operating-model.md for that half of the problem.
#
# Only fires on a brand-new session (matcher "startup"), not resume/clear/
# compact/fork, so it never resets an in-flight session's budget.
#
# FAIL-OPEN: any error here must never block a session from starting.

$ErrorActionPreference = 'SilentlyContinue'

$queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
$marker = Join-Path $queueDir "AUTOPILOT"
$offSwitch = Join-Path $queueDir "AUTOPILOT.OFF"

# Respect the maintainer's off switch unconditionally - never re-arm over it.
if (Test-Path $offSwitch) { exit 0 }

if (-not (Test-Path $queueDir)) {
    try { New-Item -ItemType Directory -Path $queueDir -Force | Out-Null } catch { exit 0 }
}

# 4 hours / 20 iterations, matching the documented default stretch in
# docs/agent-operating-model.md. Always refreshed on a fresh session start,
# even if a marker already exists, so a new session gets its own full budget
# rather than inheriting whatever was left of a previous one's expiry.
$expiresAt = [DateTimeOffset]::UtcNow.AddHours(4).ToUnixTimeSeconds()
$cfg = @{ expires_at = $expiresAt; max_iterations = 20 } | ConvertTo-Json -Compress
try { $cfg | Set-Content $marker -Encoding utf8 -ErrorAction Stop } catch { exit 0 }

$context = "Autopilot armed automatically for this session (expires in 4h, max 20 continuations - see docs/agent-operating-model.md). Read docs/BACKLOG.md and start on the top unblocked item without waiting to be asked. Do not stop at the end of a task to ask whether to continue; the Stop hook will push back on that. Stop only for a genuine human decision (see the operating model doc's 'what still needs a human' section)."

$out = @{
    hookSpecificOutput = @{
        hookEventName    = "SessionStart"
        additionalContext = $context
    }
} | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
