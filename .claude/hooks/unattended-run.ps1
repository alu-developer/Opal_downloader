# Runs one unattended `claude` session and keeps the machine awake while it
# works. Started by resume-runner.ps1; not useful on its own.
#
# WHY THIS EXISTS (2026-07-29 post-mortem)
# ----------------------------------------
# resume-runner.ps1 launched a run at 21:45 on 2026-07-28. The transcript ends
# at 21:46:43, mid-sentence, one second after the agent started a 30-minute
# background `go test` and said "I'll continue once it reports back". Its
# stdout log is 0 bytes. Nothing was committed. Nothing recorded that anything
# had gone wrong.
#
# The machine had entered Modern Standby. On this laptop that is the normal
# idle state, not a rare event - the System log shows it entering and leaving
# standby several times a night. So the runner's whole job ("resume work while
# nobody is watching") was being handed to a process that gets frozen ~90
# seconds later, on a machine that is by definition idle because nobody is
# watching. The one launch path that worked was self-defeating.
#
# Two things follow, and this script is both of them:
#
#   1. HOLD THE SYSTEM AWAKE for as long as the run lasts.
#      SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED) is the
#      documented way to say "I am doing work, do not idle-sleep". Deliberately
#      NOT ES_DISPLAY_REQUIRED: the screen may and should go dark. And it only
#      blocks IDLE sleep - closing the lid or pressing the power button still
#      sleeps the machine, which is correct. The maintainer keeping their own
#      sleep button is not a bug.
#
#   2. REPORT WHAT HAPPENED. The runner logs "launched" and then never mentions
#      the run again, so a run that dies after 90 seconds looks exactly like a
#      run that worked. This waits for the process and writes one more line:
#      exit code, wall clock, bytes produced, and whether the tree gained any
#      commits. That line is the difference between "autopilot is running" and
#      "autopilot has been dead since Tuesday".
#
# Exits with the agent's own exit code. Always releases the wake lock, even if
# the run throws - a leaked ES_CONTINUOUS would keep the laptop awake forever.

param(
    # The resolved claude launcher (.cmd/.exe). See Resolve-ClaudeLauncher in
    # resume-runner.ps1 for why the bare name will not do.
    [Parameter(Mandatory = $true)][string]$ClaudeCmd,
    # File holding the prompt, fed in over stdin.
    [Parameter(Mandatory = $true)][string]$PromptFile,
    # Where the agent's stdout goes. stderr goes to "$RunLog.err".
    [Parameter(Mandatory = $true)][string]$RunLog,
    # For the shared decision log.
    [Parameter(Mandatory = $true)][string]$QueueDir,
    # Repo root, so the outcome line can say whether anything was committed.
    [string]$RepoRoot,
    # Skip the wake lock. For the test suite, which must not pin a developer's
    # laptop awake just to assert on the reporting.
    [switch]$NoWakeLock
)

$ErrorActionPreference = 'SilentlyContinue'
Set-StrictMode -Off

$logPath = Join-Path $QueueDir "resume-runner.log"
function Say { param([string]$Decision, [string]$Detail = "")
    $stamp = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $line = "$stamp`t$Decision"
    if ($Detail) { $line += "`t$Detail" }
    Write-Output $line
    try { Add-Content -Path $logPath -Value $line -Encoding utf8 } catch { }
}

# --- wake lock ----------------------------------------------------------------
# [uint32] casts are load-bearing: PowerShell parses 0x80000000 as Int32, so the
# literal arrives as -2147483648 and the P/Invoke marshaller throws "value was
# either too large or too small for a UInt32". Silently, under
# SilentlyContinue - which would leave the run unprotected and looking fine.
$ES_CONTINUOUS = [uint32]2147483648
$ES_SYSTEM_REQUIRED = [uint32]1
$wakeHeld = $false
if (-not $NoWakeLock) {
    try {
        Add-Type -Namespace OpalPower -Name Native -MemberDefinition @'
[DllImport("kernel32.dll", SetLastError=true)]
public static extern uint SetThreadExecutionState(uint esFlags);
'@ -ErrorAction Stop
        # Returns the PREVIOUS state, or 0 on failure. Nonzero is the only
        # confirmation available without admin rights (`powercfg /requests`
        # needs elevation), so it is what the test suite asserts on.
        $prev = [OpalPower.Native]::SetThreadExecutionState($ES_CONTINUOUS -bor $ES_SYSTEM_REQUIRED)
        $wakeHeld = ($prev -ne 0)
        if (-not $wakeHeld) { Say "wake-lock-failed" "SetThreadExecutionState returned 0; the run may be frozen by Modern Standby" }
    } catch {
        Say "wake-lock-failed" $_.Exception.Message
    }
}

$startedAt = Get-Date
$headCommit = ""
if ($RepoRoot) { $headCommit = (& git -C $RepoRoot rev-parse HEAD 2>$null) }

$exitCode = -1
try {
    $proc = Start-Process -FilePath $ClaudeCmd `
        -ArgumentList @('-p', '--model', 'sonnet', '--dangerously-skip-permissions') `
        -WorkingDirectory $(if ($RepoRoot) { $RepoRoot } else { (Get-Location).Path }) `
        -RedirectStandardInput $PromptFile `
        -RedirectStandardOutput $RunLog `
        -RedirectStandardError "$RunLog.err" `
        -WindowStyle Hidden -PassThru -ErrorAction Stop
    # -Wait on Start-Process is not used on purpose: it does not compose with
    # the redirects above on Windows PowerShell 5.1 in the presence of a
    # -RedirectStandardInput file, and WaitForExit gives the pid for the
    # outcome line either way.
    #
    # Touching .Handle is NOT dead code. Without it the Process object never
    # caches the process handle, Windows recycles it at exit, and .ExitCode
    # comes back EMPTY - so every run, healthy or dead, got reported as
    # "run-error, exit ". Caught by the tests below on the first run, which is
    # the whole argument for having written them.
    $null = $proc.Handle
    $proc.WaitForExit()
    $exitCode = $proc.ExitCode
    if ($null -eq $exitCode) { $exitCode = -1 }
} catch {
    Say "run-failed" $_.Exception.Message
} finally {
    if ($wakeHeld) { [OpalPower.Native]::SetThreadExecutionState($ES_CONTINUOUS) | Out-Null }
}

# --- the outcome line ---------------------------------------------------------
$mins = [math]::Round(((Get-Date) - $startedAt).TotalMinutes, 1)
$bytes = 0
try { $bytes = (Get-Item $RunLog -ErrorAction Stop).Length } catch { }
$commits = "?"
if ($RepoRoot -and $headCommit) {
    $nowHead = (& git -C $RepoRoot rev-parse HEAD 2>$null)
    if ($nowHead) {
        if ($nowHead -eq $headCommit) { $commits = "0" }
        else { $commits = (& git -C $RepoRoot rev-list --count "$headCommit..$nowHead" 2>$null) }
    }
}

# Whether the run left anything behind that only exists in the working tree,
# and whether what it did commit ever left the machine. 2>$null, never 2>&1:
# see the note in scripts/test-hooks.ps1 about NativeCommandError.
$dirty = $false
$unpushed = "?"
if ($RepoRoot) {
    $porcelain = @(& git -C $RepoRoot status --porcelain 2>$null | Where-Object { $_ })
    $dirty = ($porcelain.Count -gt 0)
    $ahead = (& git -C $RepoRoot rev-list --count '@{u}..HEAD' 2>$null)
    if ($ahead -match '^\d+$') { $unpushed = $ahead }
}

# A run that ends in under two minutes having written nothing is the signature
# of the 2026-07-28 death, not of a quick success. Name it, so the next person
# reading this log does not have to reconstruct it from transcripts again.
#
# run-left-uncommitted is the 2026-07-30 signature, and it cost a day. That run
# went 9.3 minutes, wrote 122 bytes, exited 0 and was logged as "finished" -
# while the fix it had written sat uncommitted in the working tree and the
# verification it launched died with the run's own process. "0 new commit(s)"
# was right there in the outcome line and read as unremarkable. An unattended
# run that changes files and commits nothing has produced nothing durable: the
# machine is unattended, so a working tree is not a place work survives.
$verdict = "finished"
if ($exitCode -ne 0) { $verdict = "run-error" }
elseif ($mins -lt 2 -and $bytes -eq 0) { $verdict = "run-died-early" }
elseif ($commits -eq "0" -and $dirty) { $verdict = "run-left-uncommitted" }
Say $verdict "exit $exitCode, ${mins}m, ${bytes}B stdout, $commits new commit(s), $unpushed unpushed"

exit $exitCode
