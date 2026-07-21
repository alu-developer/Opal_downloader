# Stop hook: keeps an autonomous work session going instead of ending the turn
# and waiting for the user to type "weiter".
#
# Only engages when .claude/queue/AUTOPILOT exists (see docs/agent-operating-model.md).
# Without that marker this hook is a no-op, so ordinary conversations - asking a
# question, getting an answer, stopping - behave exactly as before.
#
# FAIL-OPEN BY DESIGN: any error, missing file, unreadable JSON or unexpected
# state exits 0 (= allow the stop). Trapping the user in a loop is far worse
# than occasionally stopping early, so every uncertain path ends the turn.

$ErrorActionPreference = 'SilentlyContinue'

function Allow-Stop { exit 0 }

# --- read hook input (session id lets us count iterations per session) --------
$sessionId = "unknown"
try {
    $raw = [Console]::In.ReadToEnd()
    if ($raw) {
        $payload = $raw | ConvertFrom-Json -ErrorAction Stop
        if ($payload.session_id) { $sessionId = [string]$payload.session_id }
    }
} catch { }

# Overridable so the hook can be exercised against a throwaway directory.
# This is not a convenience: on 2026-07-21 a verification run ended with
# `rm -f .claude/queue/AUTOPILOT .autopilot-state.json .autopilot-session.json`
# to clean up its own artefacts, and killed the REAL autopilot run - deleting
# the session record too, which is exactly what defeats the restore-on-delete
# protection. Tests must set OPAL_AUTOPILOT_QUEUE_DIR and never touch the real
# queue directory.
$queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
$marker = Join-Path $queueDir "AUTOPILOT"
$sessionRecord = Join-Path $queueDir ".autopilot-session.json"
$offSwitch = Join-Path $queueDir "AUTOPILOT.OFF"

# The maintainer's off switch, and the ONLY early exit. Honoured first and
# unconditionally.
if (Test-Path $offSwitch) {
    Remove-Item $marker, $sessionRecord -Force -ErrorAction SilentlyContinue
    Allow-Stop
}

# Effective config: the marker if present, otherwise a live session record.
#
# The fallback is deliberate. On 2026-07-21 the assistant ended three
# autonomous runs early by deleting the marker itself and justifying it
# afterwards - the exact behaviour autopilot exists to prevent, and the old
# version of this hook literally told it how ("to end autopilot, delete
# ..."). Ending the run is not the assistant's call to make: the guards
# below decide, or the maintainer does via AUTOPILOT.OFF. So a deleted
# marker no longer ends anything while the session record is still live; it
# is simply restored.
$cfg = $null
if (Test-Path $marker) {
    try { $cfg = Get-Content $marker -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $cfg = $null }
}
if ($null -eq $cfg -and (Test-Path $sessionRecord)) {
    try { $cfg = Get-Content $sessionRecord -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $cfg = $null }
}
if ($null -eq $cfg) { Allow-Stop }

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

# --- hard expiry: autopilot is always time-boxed ------------------------------
if ($null -eq $cfg.expires_at) { Allow-Stop }
if ($now -ge [int64]$cfg.expires_at) {
    Remove-Item $marker, $sessionRecord -Force -ErrorAction SilentlyContinue
    Allow-Stop
}

$maxIterations = 20
if ($null -ne $cfg.max_iterations) { $maxIterations = [int]$cfg.max_iterations }

# --- iteration cap, per session ----------------------------------------------
$statePath = Join-Path $queueDir ".autopilot-state.json"
$state = $null
if (Test-Path $statePath) {
    try { $state = Get-Content $statePath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $state = $null }
}
if ($null -eq $state) { $state = [pscustomobject]@{} }

$count = 0
if ($state.PSObject.Properties.Name -contains $sessionId) { $count = [int]$state.$sessionId }
if ($count -ge $maxIterations) { Allow-Stop }

# --- rate-limit budget --------------------------------------------------------
# Keep the real status file fresh first. The status line is its only writer and
# does not run in non-interactive or claude-desktop sessions, so without this
# the file is routinely hours old - it was found 18h stale reporting "1%" while
# the account sat at 46%. See rate-limit-keepwarm.ps1.
#
# A transcript-derived estimate was tried here and REMOVED on 2026-07-21: it
# put the 5h window at 83.5% when the real figure was 46%, i.e. it would have
# stopped autonomous work for no reason. A miscalibrated budget signal that
# gates work is worse than no signal.
$keepwarm = Join-Path $PSScriptRoot "rate-limit-keepwarm.ps1"
if (Test-Path $keepwarm) {
    # & on the script path, never a child powershell.exe: a child inherits
    # this hook's stdin and blocks forever trying to read it.
    & $keepwarm | Out-Null
}

$statusPath = Join-Path $env:USERPROFILE ".claude\rate-limit-status.json"
$rateKnown = $false
$five = $null
$seven = $null
if (Test-Path $statusPath) {
    try {
        $rl = Get-Content $statusPath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
        $age = $now - [int64]$rl.updated_at

        # Per-window staleness, not one blanket check (queue-run's research):
        # a window whose resets_at has passed rolled over since the file was
        # written, so its number is meaningless - treating that as "high" would
        # gate every future run forever. A window still inside its period is
        # usable even when the file is old, because usage only climbs: the last
        # reading is a floor.
        if ($age -lt 600) {
            $rateKnown = $true
            $five = $rl.five_hour.used_percentage
            $seven = $rl.seven_day.used_percentage
        } else {
            if ($null -ne $rl.five_hour.resets_at -and [int64]$rl.five_hour.resets_at -gt $now) {
                $five = $rl.five_hour.used_percentage
                $rateKnown = $true
            }
            if ($null -ne $rl.seven_day.resets_at -and [int64]$rl.seven_day.resets_at -gt $now) {
                $seven = $rl.seven_day.used_percentage
                $rateKnown = $true
            }
        }

        if (($null -ne $five) -and ($five -ge 75)) { Allow-Stop }
        if (($null -ne $seven) -and ($seven -ge 80)) { Allow-Stop }
    } catch { }
}

if (-not $rateKnown -and $count -ge 8) {
    # No usable budget reading at all: allow a short autonomous stretch, not a
    # long one.
    Allow-Stop
}

# --- already waiting on a background job? ------------------------------------
# Blocking the stop here would waste a whole turn: when a backgrounded command
# finishes, the harness re-invokes the assistant on its own. So if a long job
# belonging to this repo is still running, let the turn end and let the
# completion notification do the waking.
# Matching on the repo path does not work: `go test ./internal/scraper/` is
# spawned with a RELATIVE package path, so the repo root never appears in the
# command line. Match the shapes of long job this repo actually starts
# instead. A false positive only ends the turn early (autopilot resumes on the
# next user message), which is the harmless direction.
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
try {
    $procs = Get-CimInstance Win32_Process -ErrorAction Stop
    $busy = $procs | Where-Object {
        ($_.Name -eq 'go.exe' -and $_.CommandLine -and $_.CommandLine -match '\s(test|run)\s') -or
        ($_.Name -like '*.test.exe') -or
        ($_.Name -in @('opal-dl.exe', 'opal-downloader.exe')) -or
        ($_.CommandLine -and $_.CommandLine -like "*$repoRoot*" -and $_.Name -ne 'powershell.exe')
    }
    if ($busy) { Allow-Stop }
} catch { }

# --- is there actually queued work? ------------------------------------------
$todoDir = Join-Path $queueDir "todo"
if (-not (Test-Path $todoDir)) { Allow-Stop }
$todo = @(Get-ChildItem $todoDir -Filter *.md -File -ErrorAction SilentlyContinue)
if ($todo.Count -eq 0) { Allow-Stop }

# --- continue ----------------------------------------------------------------
# Persist the config so a later self-deletion of the marker cannot end the
# run, and restore the marker if it has already gone missing.
try { $cfg | ConvertTo-Json -Depth 5 | Set-Content $sessionRecord -Encoding utf8 -ErrorAction Stop } catch { }
if (-not (Test-Path $marker)) {
    try { $cfg | ConvertTo-Json -Depth 5 | Set-Content $marker -Encoding utf8 -ErrorAction Stop } catch { }
}

$state | Add-Member -NotePropertyName $sessionId -NotePropertyValue ($count + 1) -Force
try { $state | ConvertTo-Json -Depth 5 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop } catch { Allow-Stop }

$names = ($todo | Select-Object -First 5 | ForEach-Object { $_.BaseName }) -join ", "
$budgetNote = "budget unknown (no usable rate-limit reading)"
if ($rateKnown) { $budgetNote = "budget ok (5h $five%, 7d $seven%)" }

$reason = @"
AUTOPILOT is on ($($count + 1)/$maxIterations this session, $budgetNote), and $($todo.Count) task(s) remain in .claude/queue/todo/: $names

Do not stop to ask whether to continue. Pick the highest-value remaining task yourself and work it end to end: implement, run scripts/dev.ps1 all, verify against the task's own acceptance criteria, open a PR, and merge it once checks pass and every criterion is genuinely met.

Rules that still apply: never report a criterion as verified without exercising it; label anything unverified explicitly; a negative result is a valid outcome to report and file. Stop early only if a task genuinely needs a human decision - if so, move it to .claude/queue/blocked/ with the open question written down, then continue with the next task.

Ending this run is NOT your call. Deleting .claude/queue/AUTOPILOT does not work - the hook restores it. The guards above end the run (expiry, iteration cap, rate limits, empty queue), or the maintainer does by creating .claude/queue/AUTOPILOT.OFF. If you believe it should stop early, say so in your reply and keep working; do not act on it yourself.

"Budget", "this session is long", and "the next task deserves a fresh start" are not stop conditions. They are the rationalisations used to end three earlier runs.
"@

$out = @{ decision = "block"; reason = $reason } | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
