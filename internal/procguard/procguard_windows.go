//go:build windows

// Package procguard ties the lifetime of child processes (specifically, the
// Chromium/Brave process Playwright launches for login/crawl) to this
// process's own lifetime, so a visible browser window can never outlive the
// opal-downloader process that opened it - not even when that process is
// killed abruptly (crash, `taskkill /F`, a queue-run force-stop) rather than
// exiting normally.
//
// Background: session.go's ensureSession launches a non-headless Chromium
// for interactive login and reuses the same browser/context for the
// following crawl; every CLI command (root.go) and the GUI's sync job
// (gui/sync.go) already `defer sc.Close()` to close it on normal
// return/error/panic. That covers graceful completion, but Go's `defer`
// simply does not run when a process is terminated at the OS level (SIGKILL
// on POSIX, TerminateProcess/`taskkill /F` on Windows) - which is exactly
// what happens when an interactive login task gets force-stopped mid-run.
// The result: an orphaned, ownerless Chromium/Brave window stays open
// indefinitely.
//
// EnsureChildProcessesDieWithParent closes that gap at the OS level instead
// of relying on Go code running at all: it creates a Windows Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and assigns this process to it. Windows
// then guarantees every process in the job (this one and, by default
// inheritance, every child it spawns - including the Chromium process
// Playwright launches) is terminated the moment the job's last handle
// closes, which happens automatically when this process exits for any
// reason, including a hard kill. Nested jobs are supported since Windows 8
// (this project's practical minimum), so this works even though Chromium
// creates its own job objects for sandboxing its child processes.
//
// This behavior - specifically, that a descendant process actually dies
// when the process that called EnsureChildProcessesDieWithParent is
// force-killed (not just when it exits gracefully) - is covered by an
// automated regression test: see
// TestEnsureChildProcessesDieWithParent_KillsGrandchildOnForceKill in
// procguard_windows_test.go. It force-kills a helper subprocess and asserts
// a grandchild process it spawned dies too, reproducing the exact scenario
// PR #53 (commit 30f5fd0) fixed and previously only verified once by hand.
package procguard

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobObjectExtendedLimitInformation is JobObjectExtendedLimitInformation,
// the SetInformationJobObject information class used to set
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
const jobObjectExtendedLimitInformation = 9

// jobObjectLimitKillOnJobClose is JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE:
// terminates all processes associated with the job when the last handle to
// the job is closed (including implicitly, on process exit/crash/force-kill).
const jobObjectLimitKillOnJobClose = 0x00002000

// jobobjectBasicLimitInformation mirrors JOBOBJECT_BASIC_LIMIT_INFORMATION.
// Only LimitFlags is used; the rest are zero-valued fields Windows ignores
// unless the corresponding flag is set.
type jobobjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

// ioCounters mirrors IO_COUNTERS, an opaque-to-us struct embedded in
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION that Windows requires the caller to
// size correctly even though this code never reads or sets it.
type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

// jobobjectExtendedLimitInformation mirrors
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION.
type jobobjectExtendedLimitInformation struct {
	BasicLimitInformation jobobjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// EnsureChildProcessesDieWithParent creates a Windows Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and assigns the current process to it,
// so every child process this one spawns (in particular, the Chromium/Brave
// process Playwright launches for login/crawl) is forcibly terminated the
// instant this process exits or is killed, by any means. Intended to be
// called once, early in main/Execute, before any browser is launched.
//
// Best-effort: if job creation or assignment fails (e.g. this process is
// already in a job that forbids being placed in another on older Windows
// versions without nested-job support), it is logged-and-ignored rather than
// treated as fatal - losing this hardening should never block
// login/sync/list from running, since the existing defer-based cleanup still
// covers the graceful-exit case on its own.
func EnsureChildProcessesDieWithParent() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil || job == 0 {
		return
	}
	// Intentionally not closed: closing this handle immediately would
	// terminate every process still in the job (including this one), and
	// the point is for the job's lifetime to track the process's for as
	// long as it runs. Windows itself closes the handle (and, per
	// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, kills anything still in the job)
	// when this process exits.

	info := jobobjectExtendedLimitInformation{
		BasicLimitInformation: jobobjectBasicLimitInformation{
			LimitFlags: jobObjectLimitKillOnJobClose,
		},
	}
	_, _, _ = procSetInformationJobObject.Call(
		uintptr(job),
		jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)

	_ = windows.AssignProcessToJobObject(job, windows.CurrentProcess())
}

var (
	modkernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procSetInformationJobObject = modkernel32.NewProc("SetInformationJobObject")
)
