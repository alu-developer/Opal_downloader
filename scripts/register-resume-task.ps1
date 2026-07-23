# Registers (or removes) the Windows Scheduled Task that lets work resume by
# itself after the token budget recovers.
#
# The task runs .claude/hooks/resume-runner.ps1 hourly. That script is the one
# with all the judgement in it - every gate there runs in PowerShell and costs
# no tokens, so a quiet hour is free. See its header.
#
# WHY A SCHEDULED TASK AND NOT A CRON JOB
# ---------------------------------------
# An in-session CronCreate job rescues a session that is still open: a usage
# limit kills the TURN, not the session, and cron fires while the REPL is idle.
# That is genuinely useful and worth having too.
#
# But it dies with the session. Close the terminal, reboot, or let the 7-day
# expiry hit, and it is gone - silently. This task survives all of that, which
# is what the maintainer actually asked for ("I don't want to have to open a
# new session again").
#
# RUNS ONLY WHEN LOGGED ON, BY DESIGN. Running whether-logged-on-or-not means
# storing the account password in Task Scheduler. Not worth it for a
# convenience feature on a personal machine.
#
# Usage:
#   scripts/register-resume-task.ps1            # register (or update)
#   scripts/register-resume-task.ps1 -Remove    # unregister
#   scripts/register-resume-task.ps1 -Status    # show current state
#
# The off switch for the behaviour (as opposed to the plumbing) is
# .claude/queue/AUTOPILOT.OFF - same one that disables autopilot.

param(
    [switch]$Remove,
    [switch]$Status,
    # How often the gate runs. Cheap, because a no-op costs nothing.
    [int]$IntervalMinutes = 60
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Off

$taskName = "OpalDownloader-ResumeRunner"
$repoRoot = Split-Path $PSScriptRoot -Parent
$runner = Join-Path $repoRoot ".claude\hooks\resume-runner.ps1"

if ($Status) {
    $t = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if (-not $t) { Write-Host "not registered"; exit 0 }
    $info = Get-ScheduledTaskInfo -TaskName $taskName -ErrorAction SilentlyContinue
    Write-Host "task     : $taskName"
    Write-Host "state    : $($t.State)"
    Write-Host "last run : $($info.LastRunTime)  (result $($info.LastTaskResult))"
    Write-Host "next run : $($info.NextRunTime)"
    $log = Join-Path $repoRoot ".claude\queue\resume-runner.log"
    if (Test-Path $log) {
        Write-Host "`nlast 10 decisions:"
        Get-Content $log -Tail 10 | ForEach-Object { Write-Host "  $_" }
    }
    exit 0
}

if ($Remove) {
    if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
        Write-Host "removed $taskName"
    } else {
        Write-Host "$taskName was not registered"
    }
    exit 0
}

if (-not (Test-Path $runner)) { throw "resume runner not found at $runner" }

# schtasks /Create /XML, not Register-ScheduledTask.
#
# The cmdlet fails with a bare "Access is denied" here even though nothing
# about this task needs elevation. internal/scheduler/scheduler_windows.go
# already registers the app's own sync task on this machine unelevated, via
# schtasks with an XML definition - so rather than debug the cmdlet, this
# mirrors the approach the repo has already proven works.
#
# Also copied from there: InteractiveToken + LeastPrivilege (runs on the
# user's own logged-on token, no stored password), and UTF-16 encoding, which
# Microsoft's own schtasks /XML documentation calls for to avoid
# locale-dependent parse failures.
$esc = { param($s) [System.Security.SecurityElement]::Escape($s) }
$taskArgs = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$runner`""
$startBoundary = (Get-Date).AddMinutes(2).ToString("yyyy-MM-ddTHH:mm:ss")

# No <Duration> inside <Repetition> means "repeat indefinitely". Supplying
# [TimeSpan]::MaxValue instead - which is what the cmdlet path serialised -
# produces P99999999DT23H59M59S, which Task Scheduler rejects outright.
$xml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Resumes opal-downloader work automatically once the Claude token budget has recovered. Every gate runs in PowerShell and costs no tokens; see .claude/hooks/resume-runner.ps1. Disable by creating .claude/queue/AUTOPILOT.OFF.</Description>
  </RegistrationInfo>
  <Triggers>
    <TimeTrigger>
      <Repetition>
        <Interval>PT${IntervalMinutes}M</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
      <StartBoundary>$startBoundary</StartBoundary>
      <Enabled>true</Enabled>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>$(& $esc "$env:USERDOMAIN\$env:USERNAME")</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <ExecutionTimeLimit>PT3H</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>powershell.exe</Command>
      <Arguments>$(& $esc $taskArgs)</Arguments>
      <WorkingDirectory>$(& $esc $repoRoot)</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
"@

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "opal-resume-task-$([guid]::NewGuid().ToString('N').Substring(0,8)).xml"
try {
    [System.IO.File]::WriteAllText($tmp, $xml, [System.Text.Encoding]::Unicode)
    $out = & schtasks /Create /TN $taskName /XML $tmp /F 2>&1
    if ($LASTEXITCODE -ne 0) { throw "schtasks /Create failed: $out" }
} finally {
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
}

# Read it back rather than trusting the call. The first version of this script
# used Register-ScheduledTask, which reported an XML validation failure as a
# NON-terminating error - so it printed "registered" over the top of a task
# that did not exist. That is the same silent-absence bug this whole hook
# family was written to stamp out, so the check stays even now the cause is
# gone.
$check = & schtasks /Query /TN $taskName 2>&1
if ($LASTEXITCODE -ne 0) { throw "registration reported success but $taskName does not exist" }

Write-Host "registered $taskName (every $IntervalMinutes min, while logged on) - verified present"
Write-Host "  runner : $runner"
Write-Host "  off    : create .claude/queue/AUTOPILOT.OFF"
Write-Host "  status : scripts/register-resume-task.ps1 -Status"
