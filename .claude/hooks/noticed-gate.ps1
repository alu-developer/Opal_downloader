# Stop hook: asks, once per session, for one thing that was noticed and not done.
#
# WHY
# ---
# The maintainer's complaint (2026-07-27): "warum muss ich immer klar sagen,
# mach so mach so, aber du tust nie darüber nachdenken, ob das sinnvoll ist oder
# was du sonst noch so vielleicht machen solltest". They were right. Over a
# whole session every task came from their message, and the only things found
# independently were bugs in code written minutes earlier - cleanup, not
# initiative.
#
# The diagnosis was that noticing is cheap and reporting it is cheaper still,
# but neither happens on its own, because delivering exactly what was asked is
# the locally safe move every single time. CLAUDE.md already asks for more; an
# instruction that asks the assistant to *want* something loses a hundred small
# decisions over a long session.
#
# So this does not ask for a disposition. It asks for one line of output, once,
# and it asks at the only moment when the whole session is visible at once: the
# end of it.
#
# WHAT IT DELIBERATELY DOES NOT DO
# --------------------------------
# It does not check that the answer is any good, and it does not fire twice.
# A gate that judged the answer would be gameable and slow; a gate that fired
# repeatedly would be a loop. One prompt, at the end, is the whole design.
#
# FAIL-OPEN, like every other Stop hook here: any error, unreadable input or
# unexpected state exits 0 and lets the turn end. Trapping someone in a stop
# loop is far worse than missing a note.

$ErrorActionPreference = 'SilentlyContinue'

# Record that this hook ran at all, so a hook that silently stops firing can be
# attributed instead of looking like "nothing to report" (see hookbeat.ps1).
# try/catch because diagnostics must never be able to break the gate itself.
try { . (Join-Path $PSScriptRoot 'hookbeat.ps1'); Write-HookBeat -Name 'noticed-gate' } catch { }

function Allow-Stop { exit 0 }

# --- session identity --------------------------------------------------------
$sessionId = "unknown"
try {
    # .TrimStart strips a UTF-8 BOM some PowerShell host paths prepend to
    # stdin, which otherwise breaks ConvertFrom-Json silently (found
    # 2026-07-31; same fix applied to every hook with this identical read).
    $raw = [Console]::In.ReadToEnd().TrimStart([char]0xFEFF)
    if ($raw) {
        $payload = $raw | ConvertFrom-Json -ErrorAction Stop
        if ($payload.session_id) { $sessionId = [string]$payload.session_id }
        # Never re-prompt on a turn that was already blocked by another hook;
        # the assistant is not stopping anyway and the ask would be noise.
        if ($payload.stop_hook_active) { Allow-Stop }
    }
} catch { Allow-Stop }

# Overridable so the hook can be exercised against a throwaway directory -
# same reasoning as autopilot-gate.ps1, which once had a verification run
# delete the real queue state.
$queueDir = $env:OPAL_NOTICED_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
$statePath = Join-Path $queueDir ".noticed-session.json"

# The maintainer's off switch, honoured first and unconditionally.
if (Test-Path (Join-Path $queueDir "AUTOPILOT.OFF")) { Allow-Stop }

# --- fire at most once per session -------------------------------------------
try {
    if (Test-Path $statePath) {
        $state = Get-Content $statePath -Raw | ConvertFrom-Json -ErrorAction Stop
        if ($state.session_id -eq $sessionId) { Allow-Stop }
    }
} catch { }

try {
    if (-not (Test-Path $queueDir)) { New-Item -ItemType Directory -Path $queueDir -Force | Out-Null }
    @{ session_id = $sessionId; asked_at = (Get-Date).ToString("o") } |
        ConvertTo-Json -Depth 3 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop
} catch {
    # If the record cannot be written, the prompt would repeat every turn.
    Allow-Stop
}

$reason = @"
Before this turn ends: name one thing you noticed but did not do.

Not a summary of what you did - something you saw and passed over. A rough
edge, a thing that looked wrong, a question nobody has asked, something a
first-time user would trip on, a doc that contradicts the code. It does not
have to be big and it does not have to be in scope.

Append it to the "## Noticed" section of docs/BACKLOG.md (create the section
after "## Next" if it is not there yet), in one or two sentences, then finish
the turn normally. Commit it with whatever else is pending.

If you genuinely cannot think of one, say that plainly instead of inventing
something - a padded list is worse than a short one. But look properly first:
over an entire session, "nothing at all" is usually a sign of not having
looked rather than of a clean codebase.

This fires once per session. It is not asking you to do the work, only to
write down that it exists.
"@

$out = @{ decision = "block"; reason = $reason } | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
