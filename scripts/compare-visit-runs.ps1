# Diffs recent crawl runs against each other, per course and per section.
#
# WHY THIS EXISTS
# ---------------
# When a run comes back with fewer files than a baseline, the number alone is
# useless: "336 instead of 345" says nothing about where the nine went, and the
# run itself reports success. On 2026-07-26 that question took hours of live
# re-running to answer - and then thirty seconds once someone thought to diff
# the visit log, which had recorded the answer all along: one section,
# "Ubungsblatter", 29 files collapsing to 20. Precisely one OPAL page, i.e. a
# show-all expansion that silently did not happen.
#
# internal/visitlog already writes every section visit with the number of files
# it contributed, across runs, forever. This just reads it.
#
# USAGE
#   scripts/compare-visit-runs.ps1                     # last 2 runs, all courses
#   scripts/compare-visit-runs.ps1 -Runs 5             # last 5 runs, totals only
#   scripts/compare-visit-runs.ps1 -Course Analysis    # per-section detail
#
# Read-only. It never writes to the log or anywhere else.

param(
    # How many recent runs to show.
    [int]$Runs = 2,
    # Restrict to one course, and show its per-section differences.
    [string]$Course = "",
    # Path to the visit log. Defaults to the download_path in config.yaml.
    [string]$LogPath = "",
    # A gap longer than this many minutes between visits starts a new run.
    [int]$RunGapMinutes = 3,
    # Ignore this many runs from the end, to inspect an older window. Useful
    # for going back to a pair you already know differed.
    [int]$Skip = 0
)

$ErrorActionPreference = "Stop"

if (-not $LogPath) {
    $configPath = Join-Path (Split-Path $PSScriptRoot -Parent) "config.yaml"
    if (-not (Test-Path $configPath)) { throw "no config.yaml at $configPath; pass -LogPath" }
    $downloadPath = ((Get-Content $configPath | Select-String '^download_path:\s*(.+)$').Matches.Groups[1].Value).Trim()
    if (-not $downloadPath) { throw "config.yaml has no download_path; pass -LogPath" }
    $LogPath = Join-Path $downloadPath ".opal-visit-log.json"
}
if (-not (Test-Path $LogPath)) { throw "no visit log at $LogPath (it appears after the first crawl)" }

# -Encoding UTF8 is not optional: the log is UTF-8 and the section titles that
# matter most here are German ("Übungsblätter"). Read as the system codepage
# they come out mojibaked, which makes the one line you are trying to read
# unreadable.
$records = (Get-Content $LogPath -Raw -Encoding UTF8 | ConvertFrom-Json).records
if (-not $records) { throw "visit log at $LogPath has no records" }
if ($Course) { $records = $records | Where-Object { $_.course -eq $Course } }
if (-not $records) { throw "no records for course '$Course'" }

# Group visits into runs by time gap. The log is append-only across runs with
# no run id, so the gap is the only boundary available - and section visits
# within one run are seconds apart while runs are minutes apart, which makes it
# an unambiguous one in practice.
$sorted = $records | Sort-Object timestamp
$grouped = @()
$current = @()
$previous = $null
foreach ($r in $sorted) {
    $t = [datetime]$r.timestamp
    if ($null -ne $previous -and ($t - $previous).TotalMinutes -gt $RunGapMinutes) {
        $grouped += ,$current
        $current = @()
    }
    $current += $r
    $previous = $t
}
if ($current.Count) { $grouped += ,$current }

if ($Skip -gt 0) {
    $keep = $grouped.Count - $Skip
    if ($keep -lt 1) { throw "-Skip $Skip leaves no runs (only $($grouped.Count) recorded)" }
    $grouped = @($grouped | Select-Object -First $keep)
}
$recent = @($grouped | Select-Object -Last $Runs)
if ($recent.Count -eq 0) { throw "no runs found" }

Write-Host ""
Write-Host "Runs (oldest first), from $LogPath" -ForegroundColor Cyan
foreach ($run in $recent) {
    $files = ($run | Measure-Object -Property files_found -Sum).Sum
    $when = ([datetime]$run[0].timestamp).ToString("yyyy-MM-dd HH:mm:ss")
    $label = if ($Course) { $Course } else { "$(($run | Select-Object -ExpandProperty course -Unique).Count) course(s)" }
    "  {0}   {1,4} files   {2,4} section visits   {3}" -f $when, $files, $run.Count, $label
}

if ($recent.Count -lt 2) { Write-Host ""; return }

# Per-section comparison of the two most recent runs. Keyed by section_url,
# which is stable across runs where the title is not always.
$older = $recent[-2]
$newer = $recent[-1]
$olderBy = @{}
foreach ($r in $older) { $olderBy[$r.section_url] = $r }
$newerBy = @{}
foreach ($r in $newer) { $newerBy[$r.section_url] = $r }

Write-Host ""
Write-Host "Section-level differences, previous run -> latest run" -ForegroundColor Cyan
$differences = 0
foreach ($url in $newerBy.Keys) {
    $new = $newerBy[$url]
    $old = $olderBy[$url]
    if ($null -eq $old) {
        Write-Host ("  NEW SECTION   {0,4} files   {1}" -f $new.files_found, $new.section_title) -ForegroundColor Yellow
        $differences++
        continue
    }
    if ($old.files_found -ne $new.files_found) {
        $delta = $new.files_found - $old.files_found
        $colour = if ($delta -lt 0) { "Red" } else { "Green" }
        $signed = if ($delta -gt 0) { "+$delta" } else { "$delta" }
        Write-Host ("  {0,4} -> {1,-4} ({2,4})   {3}" -f $old.files_found, $new.files_found, $signed, $new.section_title) -ForegroundColor $colour
        $differences++
    }
}
foreach ($url in $olderBy.Keys) {
    if (-not $newerBy.ContainsKey($url)) {
        Write-Host ("  NOT VISITED   was {0,3} files   {1}" -f $olderBy[$url].files_found, $olderBy[$url].section_title) -ForegroundColor Red
        $differences++
    }
}

if ($differences -eq 0) {
    Write-Host "  none - the two runs found the same files in the same sections" -ForegroundColor DarkGreen
} else {
    Write-Host ""
    Write-Host "A section that drops to a round number near 20 is the signature of a" -ForegroundColor DarkGray
    Write-Host "show-all expansion that did not happen: OPAL pages at ~20 rows." -ForegroundColor DarkGray
}
Write-Host ""
