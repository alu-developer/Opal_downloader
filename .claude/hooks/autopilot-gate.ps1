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

$queueDir = Join-Path $PSScriptRoot "..\queue"
$marker = Join-Path $queueDir "AUTOPILOT"
if (-not (Test-Path $marker)) { Allow-Stop }

try {
    $cfg = Get-Content $marker -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
} catch {
    Allow-Stop
}

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

# --- hard expiry: autopilot is always time-boxed ------------------------------
if ($null -eq $cfg.expires_at) { Allow-Stop }
if ($now -ge [int64]$cfg.expires_at) {
    Remove-Item $marker -Force -ErrorAction SilentlyContinue
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
# The status file is written by the status line, which does NOT run in
# non-interactive sessions - so it goes stale exactly when autopilot matters.
# Stale data is therefore treated as "unknown", and unknown tightens the
# iteration cap rather than being ignored.
$statusPath = Join-Path $env:USERPROFILE ".claude\rate-limit-status.json"
$rateKnown = $false
if (Test-Path $statusPath) {
    try {
        $rl = Get-Content $statusPath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
        $age = $now - [int64]$rl.updated_at
        if ($age -lt 1800) {
            $rateKnown = $true
            $five = $rl.five_hour.used_percentage
            $seven = $rl.seven_day.used_percentage
            if (($null -ne $five) -and ($five -ge 75)) { Allow-Stop }
            if (($null -ne $seven) -and ($seven -ge 80)) { Allow-Stop }
        }
    } catch { }
}
if (-not $rateKnown -and $count -ge 8) {
    # Flying blind on budget: allow a short autonomous stretch, not a long one.
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
$state | Add-Member -NotePropertyName $sessionId -NotePropertyValue ($count + 1) -Force
try { $state | ConvertTo-Json -Depth 5 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop } catch { Allow-Stop }

$names = ($todo | Select-Object -First 5 | ForEach-Object { $_.BaseName }) -join ", "
$budgetNote = "budget unknown (status file stale - status line does not run in non-interactive sessions)"
if ($rateKnown) { $budgetNote = "budget ok (5h $five%, 7d $seven%)" }

$reason = @"
AUTOPILOT is on ($($count + 1)/$maxIterations this session, $budgetNote), and $($todo.Count) task(s) remain in .claude/queue/todo/: $names

Do not stop to ask whether to continue. Pick the highest-value remaining task yourself and work it end to end: implement, run scripts/dev.ps1 all, verify against the task's own acceptance criteria, open a PR, and merge it once checks pass and every criterion is genuinely met.

Rules that still apply: never report a criterion as verified without exercising it; label anything unverified explicitly; a negative result is a valid outcome to report and file. Stop early only if a task genuinely needs a human decision - if so, move it to .claude/queue/blocked/ with the open question written down, then continue with the next task.

To end autopilot deliberately, delete .claude/queue/AUTOPILOT.
"@

$out = @{ decision = "block"; reason = $reason } | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
