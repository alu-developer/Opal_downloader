//go:build windows

package scheduler

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/text/encoding/unicode"
)

// taskDefinition mirrors just the subset of the Task Scheduler 1.2 XML
// schema (https://learn.microsoft.com/windows/win32/taskschd/task-scheduler-schema)
// this package needs, built via encoding/xml (rather than a raw string
// template) so field values are escaped correctly automatically. See
// buildTaskXML for how these map to docs/scheduled-sync-plan.md section 4's
// design choices.
type taskDefinition struct {
	XMLName          xml.Name         `xml:"Task"`
	Xmlns            string           `xml:"xmlns,attr"`
	Version          string           `xml:"version,attr"`
	RegistrationInfo registrationInfo `xml:"RegistrationInfo"`
	Triggers         triggers         `xml:"Triggers"`
	Principals       principals       `xml:"Principals"`
	Settings         taskSettings     `xml:"Settings"`
	Actions          actions          `xml:"Actions"`
}

type registrationInfo struct {
	Description string `xml:"Description"`
}

type triggers struct {
	CalendarTrigger calendarTrigger `xml:"CalendarTrigger"`
	LogonTrigger    logonTrigger    `xml:"LogonTrigger"`
}

type calendarTrigger struct {
	StartBoundary string        `xml:"StartBoundary"`
	Enabled       bool          `xml:"Enabled"`
	ScheduleByDay scheduleByDay `xml:"ScheduleByDay"`
}

type scheduleByDay struct {
	DaysInterval int `xml:"DaysInterval"`
}

// logonTrigger is the catch-up trigger added alongside calendarTrigger - see
// buildTaskXML's doc comment for why this project now registers both rather
// than relying on STARTWHENAVAILABLE alone. Scoped to the same UserId as
// principals.Principal so it fires only for this task's own user logging on,
// not any other account on a shared machine.
type logonTrigger struct {
	Enabled bool   `xml:"Enabled"`
	UserId  string `xml:"UserId,omitempty"`
}

type principals struct {
	Principal principal `xml:"Principal"`
}

type principal struct {
	ID        string `xml:"id,attr"`
	UserId    string `xml:"UserId,omitempty"`
	LogonType string `xml:"LogonType"`
	RunLevel  string `xml:"RunLevel"`
}

type taskSettings struct {
	MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
	DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
	StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
	RunOnlyIfNetworkAvailable  bool   `xml:"RunOnlyIfNetworkAvailable"`
	Enabled                    bool   `xml:"Enabled"`
	Hidden                     bool   `xml:"Hidden"`
}

type actions struct {
	Context string     `xml:"Context,attr"`
	Exec    execAction `xml:"Exec"`
}

type execAction struct {
	Command          string `xml:"Command"`
	Arguments        string `xml:"Arguments"`
	WorkingDirectory string `xml:"WorkingDirectory"`
}

// buildTaskXML renders the Task Scheduler XML definition for the daily
// scheduled-sync task, implementing every design decision in
// docs/scheduled-sync-plan.md section 4:
//
//   - CalendarTrigger with ScheduleByDay/DaysInterval=1 (fixed daily time)
//     PLUS a LogonTrigger as a catch-up backstop - see below for why both
//     are now registered, correcting section 4's original single-trigger
//     reasoning.
//   - Settings.StartWhenAvailable=true (STARTWHENAVAILABLE): Task
//     Scheduler's own built-in catch-up behavior for "machine was
//     off/asleep at the scheduled time".
//   - Principal LogonType=InteractiveToken, RunLevel=LeastPrivilege, and a
//     UserId set to the current user: the task runs using this user's own
//     already-logged-on interactive session token, i.e. only while they're
//     logged on - the simpler, safer default this task's spec calls for
//     (no Windows credential storage/stored password needed for the task
//     itself, unlike LogonType=Password/S4U would require).
//   - DisallowStartIfOnBatteries/StopIfGoingOnBatteries both false: a
//     laptop on battery should still get its daily sync rather than
//     silently skipping it, since this project has no separate "only on
//     AC power" requirement and skipping would just look like an
//     unexplained missed run.
//
// Why both a CalendarTrigger and a LogonTrigger now, reversing section 4's
// original "not an on-logon trigger" recommendation: that reasoning leaned
// on STARTWHENAVAILABLE alone giving "a logon-like catch-up for free".
// Friction campaign walk 1 (2026-08-11, docs/BACKLOG.md Finding 1) found
// that false for the case that matters most - Windows event 332 shows a
// missed run whose catch-up window falls while the user is not logged on
// at all (not merely asleep) is consumed and discarded, never deferred to
// the next logon, so real usage lost 3 of 5 days. The LogonTrigger fills
// that specific gap; MultipleInstancesPolicy=IgnoreNew (already set below)
// prevents the two triggers colliding if they fire close together, and
// cmd/opal-downloader's errAlreadySucceededToday guard (root.go) is the
// "already ran today" dedup section 4 said a logon trigger would need,
// so a machine that gets locked/unlocked repeatedly does not resync every
// time - only the first trigger of the day that finds no success yet
// today actually runs.
//
// StartBoundary uses today's date at hhmm:00 in local time (no timezone
// suffix - Task Scheduler interprets an offset-less StartBoundary as local
// time); the date component only anchors when the recurrence's *first*
// possible occurrence is computed from, not a one-time trigger, since
// ScheduleByDay/DaysInterval=1 makes this recur every day at that
// time-of-day indefinitely.
func buildTaskXML(exePath, hhmm string) ([]byte, error) {
	startTime, err := time.ParseInLocation("15:04", hhmm, time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid time %q: %w", hhmm, err)
	}
	now := time.Now()
	startBoundary := time.Date(now.Year(), now.Month(), now.Day(), startTime.Hour(), startTime.Minute(), 0, 0, time.Local)

	userID := ""
	if u, err := user.Current(); err == nil {
		userID = u.Username
	}

	def := taskDefinition{
		Xmlns:   "http://schemas.microsoft.com/windows/2004/02/mit/task",
		Version: "1.2",
		RegistrationInfo: registrationInfo{
			Description: "Daily automatic opal-downloader sync. Opt-in; added by opal-downloader itself via the GUI's Settings toggle or `opal-downloader schedule enable`. See docs/scheduled-sync-plan.md.",
		},
		Triggers: triggers{
			CalendarTrigger: calendarTrigger{
				StartBoundary: startBoundary.Format("2006-01-02T15:04:05"),
				Enabled:       true,
				ScheduleByDay: scheduleByDay{DaysInterval: 1},
			},
			LogonTrigger: logonTrigger{
				Enabled: true,
				UserId:  userID,
			},
		},
		Principals: principals{
			Principal: principal{
				ID:        "Author",
				UserId:    userID,
				LogonType: "InteractiveToken",
				RunLevel:  "LeastPrivilege",
			},
		},
		Settings: taskSettings{
			MultipleInstancesPolicy:    "IgnoreNew",
			DisallowStartIfOnBatteries: false,
			StopIfGoingOnBatteries:     false,
			StartWhenAvailable:         true,
			RunOnlyIfNetworkAvailable:  false,
			Enabled:                    true,
			Hidden:                     false,
		},
		Actions: actions{
			Context: "Author",
			Exec: execAction{
				Command:   exePath,
				Arguments: "sync --scheduled",
				// Task Scheduler launches an action with no working directory
				// set to C:\Windows\System32, not exePath's own folder. Every
				// subcommand resolves config.yaml relative to os.Getwd()
				// (projectDir() in cmd/opal-downloader/root.go), so without
				// this, a scheduled run fails with "config file not found:
				// C:\windows\system32\config.yaml" - found live on the
				// maintainer's machine (2026-07-23) even though the exe path
				// itself was already stable.
				WorkingDirectory: filepath.Dir(exePath),
			},
		},
	}

	body, err := xml.MarshalIndent(&def, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling task XML: %w", err)
	}

	var out bytes.Buffer
	out.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\r\n")
	out.Write(body)
	return out.Bytes(), nil
}

// Enable registers (creating or updating) the scheduled-sync Task Scheduler
// task to run "<exePath> sync --scheduled" daily at hhmm. Safe to call
// repeatedly (e.g. changing the time later): schtasks /Create /F overwrites
// any existing task of the same name rather than erroring that it already
// exists.
func Enable(exePath, hhmm string) error {
	if err := ValidateTime(hhmm); err != nil {
		return err
	}

	xmlContent, err := buildTaskXML(exePath, hhmm)
	if err != nil {
		return err
	}

	// Microsoft's own schtasks /Create /XML documentation notes the XML
	// file should be saved as Unicode (UTF-16) to avoid locale-dependent
	// parsing failures - encoding it that way here (rather than plain
	// UTF-8) avoids depending on the machine's current ANSI code page.
	encoded, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes(xmlContent)
	if err != nil {
		return fmt.Errorf("encoding task XML as UTF-16: %w", err)
	}

	tmp, err := os.CreateTemp("", "opal-downloader-task-*.xml")
	if err != nil {
		return fmt.Errorf("creating temporary task XML file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temporary task XML file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary task XML file: %w", err)
	}

	// HideWindow (CREATE_NO_WINDOW) avoids a console-window flash: these
	// callers run from the GUI's native WebView2 window, which has no
	// console of its own for schtasks.exe to attach to, so without this it
	// would pop open a brand new one. Same fix as internal/notify's
	// powershell.exe calls.
	cmd := exec.Command("schtasks", "/Create", "/TN", TaskName, "/XML", tmpPath, "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Create failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Disable removes the scheduled-sync Task Scheduler task. Idempotent: if
// the task doesn't exist (already disabled, or never enabled), this is a
// no-op rather than an error.
//
// This function itself performs no ownership check - it deletes whatever is
// currently registered under TaskName, full stop. That is safe today only
// because both real callers (cmd/opal-downloader/root.go's `schedule
// disable` subcommand, internal/gui/schedule.go's applyScheduleRegistration)
// are reachable exclusively via explicit user action - typing the command or
// submitting the Settings form - and nothing else in this codebase calls
// Disable automatically. Any new caller added later inherits that same
// obligation: TaskName is one global name per machine, so an automatic or
// background call here would silently remove the user's live daily sync.
func Disable() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", TaskName, "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if taskNotFound(out) {
			return nil
		}
		return fmt.Errorf("schtasks /Delete failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Status queries whether the scheduled-sync task is currently registered
// and, if so, its daily trigger time.
func Status() (Info, error) {
	cmd := exec.Command("schtasks", "/Query", "/TN", TaskName, "/XML")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if taskNotFound(out) {
			return Info{Registered: false}, nil
		}
		return Info{}, fmt.Errorf("schtasks /Query failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// Report the registered binary and whether it still exists. A task whose
	// action points at a file that has since been deleted stays "registered"
	// forever while never actually running, and nothing else in the product
	// would ever notice: the scheduled-status file is only written *by a run
	// that happened*, so the GUI keeps showing the last successful run
	// indefinitely. See scheduler.ErrEphemeralExecutable.
	exePath := parseTaskCommand(out)

	hhmm, parseErr := parseStartTime(out)
	if parseErr != nil {
		// Registered but its trigger time couldn't be parsed (e.g. someone
		// hand-edited it in Task Scheduler's own UI into a shape this
		// package doesn't recognize) - still report it as registered rather
		// than claiming it doesn't exist.
		return withExecutableState(Info{Registered: true}, exePath), nil
	}
	return withExecutableState(Info{Registered: true, Time: hhmm}, exePath), nil
}

// parseTaskCommand extracts the registered action's executable path from a
// schtasks /Query /XML response, or "" if it cannot be determined. Failure
// is deliberately silent: this is diagnostic enrichment, and a task whose
// XML cannot be parsed here should still report as registered.
func parseTaskCommand(schtasksOutput []byte) string {
	decoded, err := decodeMaybeUTF16(schtasksOutput)
	if err != nil {
		return ""
	}
	var task queriedTask
	if err := xml.Unmarshal(stripXMLDeclaration(decoded), &task); err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(task.Actions.Exec.Command), `"`)
}

// taskNotFound reports whether schtasks' output indicates the task simply
// doesn't exist (its actual English-locale message is "ERROR: The system
// cannot find the file specified.", but this matches loosely on "cannot
// find" so it doesn't depend on an exact locale/wording match).
func taskNotFound(schtasksOutput []byte) bool {
	return bytes.Contains(bytes.ToLower(schtasksOutput), []byte("cannot find"))
}

// queriedTask is the minimal shape parseStartTime needs from schtasks
// /Query /TN <name> /XML's output - the same schema buildTaskXML produces,
// but read loosely (only the one field this cares about) so it tolerates
// extra elements Task Scheduler's own re-serialization might add.
type queriedTask struct {
	Triggers struct {
		CalendarTrigger struct {
			StartBoundary string `xml:"StartBoundary"`
		} `xml:"CalendarTrigger"`
	} `xml:"Triggers"`
	Actions struct {
		Exec struct {
			Command string `xml:"Command"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

// parseStartTime extracts the "HH:MM" time-of-day from a schtasks
// /Query /XML response. schtasks prints this XML UTF-16-encoded with a BOM
// (matching what Enable writes for /Create); decodeMaybeUTF16 handles that
// transparently, also tolerating a plain UTF-8/ASCII response in case a
// future Windows version changes that.
func parseStartTime(schtasksOutput []byte) (string, error) {
	decoded, err := decodeMaybeUTF16(schtasksOutput)
	if err != nil {
		return "", err
	}
	// Once decoded, the bytes are UTF-8, but the XML declaration schtasks
	// (or buildTaskXML) wrote still literally says
	// encoding="UTF-16"/"UTF-8" - stripping the declaration line entirely
	// avoids encoding/xml refusing to parse a declared encoding it can't
	// itself transcode (it has no CharsetReader configured, and doesn't
	// need one now that the bytes are already UTF-8).
	decoded = stripXMLDeclaration(decoded)

	var task queriedTask
	if err := xml.Unmarshal(decoded, &task); err != nil {
		return "", fmt.Errorf("parsing task XML: %w", err)
	}
	boundary := task.Triggers.CalendarTrigger.StartBoundary
	if boundary == "" {
		return "", fmt.Errorf("task XML has no CalendarTrigger StartBoundary")
	}

	parsed, err := time.Parse("2006-01-02T15:04:05", boundary)
	if err != nil {
		return "", fmt.Errorf("parsing StartBoundary %q: %w", boundary, err)
	}
	return parsed.Format("15:04"), nil
}

// stripXMLDeclaration removes a leading "<?xml ... ?>" declaration, if
// present, from an already-UTF-8 XML document. See parseStartTime's doc
// comment for why this is needed.
func stripXMLDeclaration(b []byte) []byte {
	trimmed := bytes.TrimSpace(b)
	if !bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return b
	}
	if idx := bytes.Index(b, []byte("?>")); idx != -1 {
		return b[idx+2:]
	}
	return b
}

// decodeMaybeUTF16 converts b to UTF-8 if it looks like UTF-16 (detected via
// a leading byte-order mark, LE or BE), and returns it unchanged otherwise.
// schtasks' console output encoding has historically been inconsistent
// across Windows versions/locales; handling both transparently avoids this
// package's XML parsing being hostage to that.
func decodeMaybeUTF16(b []byte) ([]byte, error) {
	switch {
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(b)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder().Bytes(b)
	default:
		return b, nil
	}
}
