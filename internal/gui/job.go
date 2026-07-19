package gui

import (
	"encoding/json"
	"sync"
	"time"
)

// jobEvent is a single structured event surfaced to the browser via SSE.
// It intentionally carries enough detail to render per-item progress
// (which course/file, what stage, what error) rather than just a final
// summary line - see acceptance criteria in the gui-sync-page task.
type jobEvent struct {
	Seq          int    `json:"seq"`
	Time         string `json:"time"`
	Kind         string `json:"kind"` // "course_started" | "file_downloaded" | "file_skipped" | "error" | "log" | "done" | "cancelled" | "failed"
	Course       string `json:"course,omitempty"`
	File         string `json:"file,omitempty"`
	Message      string `json:"message,omitempty"`
	Error        string `json:"error,omitempty"`
	Downloaded   int    `json:"downloaded,omitempty"`
	Skipped      int    `json:"skipped,omitempty"`
	Errors       int    `json:"errors,omitempty"`
	CourseIndex  int    `json:"courseIndex,omitempty"`
	TotalCourses int    `json:"totalCourses,omitempty"`
}

// jobKind identifies which long-running action a job runs.
type jobKind string

const (
	jobKindSync      jobKind = "sync"
	jobKindList      jobKind = "list"
	jobKindDumpLinks jobKind = "dump-links"
)

// job tracks the state of one in-flight (or just-finished) long-running
// action triggered from the GUI (sync/list/dump-links). Only one job may
// run at a time; jobManager enforces that.
//
// Lifecycle note (deliberate, see acceptance criteria): a job is NOT tied to
// any single HTTP request's lifetime. /sync/start (and the list/dump-links
// equivalents) launch the job in a background goroutine and return
// immediately; the SSE stream at /sync/stream is just a *viewer* of the
// job's event log, and multiple viewers (or none at all, e.g. if the
// browser tab is closed) can attach/detach without affecting the job. This
// means:
//   - Closing the browser tab does NOT cancel the sync - it keeps running
//     server-side until it completes or someone calls /sync/cancel. This
//     matches CLI behavior (Ctrl-C'ing a terminal showing `sync` doesn't
//     mean much to a background job either) and avoids a half-downloaded,
//     manifest-inconsistent state from a merely-closed tab.
//   - Killing the whole `gui` server process DOES stop the job, because the
//     job runs in that process (there is no separate worker process/queue).
//     This is a real limitation worth stating plainly: there is no
//     persistence/resume of a job across a server restart. Given this is a
//     single-user local tool (bound to 127.0.0.1, no multi-tenant use case),
//     that tradeoff is acceptable - building a durable job queue for a
//     single local user would be substantial complexity for no real benefit.
//   - The explicit /sync/cancel control exists precisely so a stuck sync
//     doesn't require killing the process to escape (see cancelFunc below).
type job struct {
	mu sync.Mutex

	kind     jobKind
	running  bool
	cancelFn func()
	events   []jobEvent
	seq      int
	subs     map[chan jobEvent]struct{}
}

func newJob() *job {
	return &job{subs: map[chan jobEvent]struct{}{}}
}

// start marks the job as running under the given kind and cancel func. It
// returns false if a job is already running (callers should reject the
// start request, e.g. HTTP 409).
func (j *job) start(kind jobKind, cancelFn func()) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return false
	}
	j.running = true
	j.kind = kind
	j.cancelFn = cancelFn
	j.events = nil
	j.seq = 0
	return true
}

// finish marks the job as no longer running. It does not clear the event
// log, so a viewer that connects after completion still sees the full
// history via replay in subscribe.
func (j *job) finish() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.running = false
	j.cancelFn = nil
}

// isRunning reports whether a job is currently in flight, and which kind.
func (j *job) isRunning() (bool, jobKind) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running, j.kind
}

// cancel invokes the running job's cancel function, if any. It returns
// false if no job is running. The cancel function is expected to abort the
// in-flight scraper call (see sc.Close() usage in sync.go) rather than
// perform a graceful stop - Playwright's API offers no cooperative
// cancellation hook, so "close the browser out from under the in-flight
// call" is the only way to interrupt it promptly.
func (j *job) cancel() bool {
	j.mu.Lock()
	fn := j.cancelFn
	j.mu.Unlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}

// publish appends an event to the log and fans it out to all current
// subscribers. Slow/absent subscribers never block publish: each
// subscriber channel is buffered and publish drops the event for that
// subscriber if its buffer is full rather than blocking the job.
func (j *job) publish(e jobEvent) {
	j.mu.Lock()
	j.seq++
	e.Seq = j.seq
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	j.events = append(j.events, e)
	subs := make([]chan jobEvent, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	j.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			// Drop rather than block; the subscriber can always fetch the
			// full replay log on (re)connect.
		}
	}
}

// subscribe registers a new viewer channel and returns it along with a
// replay of all events published so far (so a viewer connecting mid-job, or
// after it finished, still sees full history). Callers must call the
// returned unsubscribe func when done.
func (j *job) subscribe() (ch chan jobEvent, replay []jobEvent, unsubscribe func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch = make(chan jobEvent, 64)
	j.subs[ch] = struct{}{}
	replay = make([]jobEvent, len(j.events))
	copy(replay, j.events)
	unsubscribe = func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		if _, ok := j.subs[ch]; ok {
			delete(j.subs, ch)
			close(ch)
		}
	}
	return ch, replay, unsubscribe
}

func (e jobEvent) marshal() []byte {
	data, err := json.Marshal(e)
	if err != nil {
		// Should not happen for this fixed, JSON-safe struct shape; fall
		// back to a minimal valid event rather than dropping the frame.
		data, _ = json.Marshal(jobEvent{Seq: e.Seq, Kind: "error", Error: "failed to encode event"})
	}
	return data
}
