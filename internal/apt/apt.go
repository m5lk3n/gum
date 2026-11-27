package apt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sentinel errors, usable with errors.Is.
var (
	ErrBusy            = errors.New("another apt job is already running")
	ErrLocked          = errors.New("the apt/dpkg lock is held by another process")
	ErrDpkgInterrupted = errors.New("dpkg was interrupted; run `dpkg --configure -a` as root to repair it")
	ErrSudo            = errors.New("sudo refused to run apt-get; check /etc/sudoers.d/gummy (see `gummy --print-sudoers`)")
	ErrTimeout         = errors.New("apt job timed out")
	ErrNoSummary       = errors.New("could not find the summary line in apt-get output")
	ErrCommandFailed   = errors.New("apt-get failed")
)

// outputTailLines is how many trailing lines of apt-get output an error includes.
const outputTailLines = 40

// Summary is apt-get's "N upgraded, N newly installed, N to remove and N not upgraded." line.
type Summary struct {
	Upgraded       int `json:"upgraded"`
	NewlyInstalled int `json:"newly_installed"`
	Removed        int `json:"removed"`
	NotUpgraded    int `json:"not_upgraded"`
}

var summaryRe = regexp.MustCompile(`(?m)^(\d+) upgraded, (\d+) newly installed, (\d+) to remove and (\d+) not upgraded\.`)

// ParseSummary extracts the summary line from apt-get output.
func ParseSummary(out string) (Summary, error) {
	m := summaryRe.FindStringSubmatch(out)
	if m == nil {
		return Summary{}, ErrNoSummary
	}
	n := make([]int, 4)
	for i := range n {
		v, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Summary{}, fmt.Errorf("parse summary: %w", err)
		}
		n[i] = v
	}
	return Summary{Upgraded: n[0], NewlyInstalled: n[1], Removed: n[2], NotUpgraded: n[3]}, nil
}

// UpdateResult is returned by Manager.Update.
type UpdateResult struct {
	// Refreshed is false when the package index was refreshed recently and the
	// network refresh was skipped (throttling).
	Refreshed   bool      `json:"refreshed"`
	LastRefresh time.Time `json:"last_refresh"`
	// Upgradable is the number of packages `apt-get upgrade` would upgrade.
	Upgradable int `json:"upgradable"`
	// KeptBack is the number of upgradable packages `apt-get upgrade` would keep back.
	KeptBack int `json:"kept_back"`
}

// JobResult is returned by Manager.Upgrade and Manager.Autoremove.
type JobResult struct {
	// Status is "completed", or "started" for async jobs.
	Status  string   `json:"status"`
	Summary *Summary `json:"summary,omitempty"`
}

// Config configures a Manager.
type Config struct {
	Runner            Runner
	UpdateMinInterval time.Duration
	JobTimeout        time.Duration
	Logger            *slog.Logger
	// Now is used for throttling; defaults to time.Now.
	Now func() time.Time
}

// Job states reported by Status.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

// JobInfo describes a running or finished apt job.
type JobInfo struct {
	Job        string    `json:"job" jsonschema:"update, upgrade or autoremove"`
	State      string    `json:"state" jsonschema:"running, succeeded or failed"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Duration   string    `json:"duration,omitempty"`
	Result     any       `json:"result,omitempty" jsonschema:"the job's result, as the synchronous tool call would have returned it"`
	Error      string    `json:"error,omitempty"`
}

// Status is returned by Manager.Status.
type Status struct {
	// Running is the job currently running, if any.
	Running *JobInfo `json:"running,omitempty"`
	// Last holds the most recent finished job of each kind, keyed by job name.
	Last map[string]JobInfo `json:"last"`
	// LastRefresh is when the package index was last refreshed by gummy.
	LastRefresh time.Time `json:"last_refresh,omitzero"`
	// NextRefresh is the earliest time apt_update will refresh the index again.
	NextRefresh time.Time `json:"next_refresh,omitzero"`
}

// Manager runs apt jobs one at a time.
type Manager struct {
	cfg Config

	mu          sync.Mutex
	running     *JobInfo
	last        map[string]JobInfo
	lastRefresh time.Time

	jobs sync.WaitGroup
}

// NewManager creates a Manager.
func NewManager(cfg Config) *Manager {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{cfg: cfg, last: map[string]JobInfo{}}
}

// Wait blocks until all running jobs have finished, or ctx is done.
func (m *Manager) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { m.jobs.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Status reports the running job, the last outcome of each job and the throttle state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Last: make(map[string]JobInfo, len(m.last)), LastRefresh: m.lastRefresh}
	if m.running != nil {
		r := *m.running
		st.Running = &r
	}
	for k, v := range m.last {
		st.Last[k] = v
	}
	if !m.lastRefresh.IsZero() {
		st.NextRefresh = m.lastRefresh.Add(m.cfg.UpdateMinInterval)
	}
	return st
}

func (m *Manager) acquire(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running != nil {
		return fmt.Errorf("%w (%s, started %s)", ErrBusy, m.running.Job, m.running.StartedAt.Format(time.RFC3339))
	}
	m.running = &JobInfo{Job: name, State: StateRunning, StartedAt: m.cfg.Now()}
	m.jobs.Add(1)
	return nil
}

// release records the outcome of the running job and frees the slot.
func (m *Manager) release(v any, err error) {
	m.mu.Lock()
	info := *m.running
	info.FinishedAt = m.cfg.Now()
	info.Duration = info.FinishedAt.Sub(info.StartedAt).Round(time.Millisecond).String()
	if err != nil {
		info.State, info.Error = StateFailed, err.Error()
	} else {
		info.State, info.Result = StateSucceeded, v
	}
	m.last[info.Job] = info
	m.running = nil
	m.mu.Unlock()
	m.jobs.Done()

	if err != nil {
		m.cfg.Logger.Error("apt job failed", "job", info.Job, "duration", info.Duration, "error", err)
	} else {
		m.cfg.Logger.Info("apt job finished", "job", info.Job, "duration", info.Duration, "result", logValue(v))
	}
}

// start runs fn in the background, detached from the caller's context so that a
// disconnecting client never interrupts dpkg halfway. It returns a channel that
// delivers fn's result.
func (m *Manager) start(name string, fn func(ctx context.Context) (any, error)) (<-chan jobOutcome, error) {
	if err := m.acquire(name); err != nil {
		return nil, err
	}
	ch := make(chan jobOutcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.JobTimeout)
		defer cancel()
		m.cfg.Logger.Info("apt job started", "job", name)
		v, err := fn(ctx)
		// Release before delivering the result, so a caller can start the next job right away.
		m.release(v, err)
		ch <- jobOutcome{v, err}
	}()
	return ch, nil
}

type jobOutcome struct {
	value any
	err   error
}

// wait waits for a job's outcome. If ctx ends first, the job keeps running in the background.
func wait[T any](ctx context.Context, ch <-chan jobOutcome) (T, error) {
	var zero T
	select {
	case o := <-ch:
		if o.err != nil {
			return zero, o.err
		}
		return o.value.(T), nil
	case <-ctx.Done():
		return zero, fmt.Errorf("stopped waiting (the job continues in the background): %w", ctx.Err())
	}
}

// Update refreshes the package index, unless that happened less than
// UpdateMinInterval ago, and reports how many packages can be upgraded.
func (m *Manager) Update(ctx context.Context) (UpdateResult, error) {
	ch, err := m.start("update", func(ctx context.Context) (any, error) {
		res := UpdateResult{}
		now := m.cfg.Now()
		m.mu.Lock()
		last := m.lastRefresh
		m.mu.Unlock()
		if last.IsZero() || now.Sub(last) >= m.cfg.UpdateMinInterval {
			if _, err := m.run(ctx, true, argsUpdate); err != nil {
				return nil, err
			}
			last = now
			m.mu.Lock()
			m.lastRefresh = now
			m.mu.Unlock()
			res.Refreshed = true
		}
		res.LastRefresh = last
		out, err := m.run(ctx, false, argsSimulateUpgrade)
		if err != nil {
			return nil, err
		}
		s, err := ParseSummary(out)
		if err != nil {
			return nil, err
		}
		res.Upgradable, res.KeptBack = s.Upgraded, s.NotUpgraded
		return res, nil
	})
	if err != nil {
		return UpdateResult{}, err
	}
	return wait[UpdateResult](ctx, ch)
}

// Upgrade runs `apt-get upgrade -y`. With async, it returns as soon as the job has started.
func (m *Manager) Upgrade(ctx context.Context, async bool) (JobResult, error) {
	return m.job(ctx, "upgrade", argsUpgrade, async)
}

// Autoremove runs `apt-get autoremove -y`. With async, it returns as soon as the job has started.
func (m *Manager) Autoremove(ctx context.Context, async bool) (JobResult, error) {
	return m.job(ctx, "autoremove", argsAutoremove, async)
}

func (m *Manager) job(ctx context.Context, name string, args []string, async bool) (JobResult, error) {
	ch, err := m.start(name, func(ctx context.Context) (any, error) {
		out, err := m.run(ctx, true, args)
		if err != nil {
			return nil, err
		}
		res := JobResult{Status: "completed"}
		if s, err := ParseSummary(out); err == nil {
			res.Summary = &s
		}
		return res, nil
	})
	if err != nil {
		return JobResult{}, err
	}
	if async {
		return JobResult{Status: "started"}, nil
	}
	return wait[JobResult](ctx, ch)
}

func (m *Manager) run(ctx context.Context, privileged bool, args []string) (string, error) {
	out, code, err := m.cfg.Runner.Run(ctx, privileged, args...)
	if err == nil {
		return out, nil
	}
	return out, classify(ctx, out, code, err)
}

// classify turns a failed apt-get run into an error that tells the caller what to do.
func classify(ctx context.Context, out string, code int, err error) error {
	var kind error
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		kind = ErrTimeout
	case strings.Contains(out, "dpkg was interrupted"):
		kind = ErrDpkgInterrupted
	case strings.Contains(out, "Could not get lock"),
		strings.Contains(out, "Unable to acquire the dpkg frontend lock"),
		strings.Contains(out, "Unable to lock"):
		kind = ErrLocked
	case strings.HasPrefix(out, "sudo:") || strings.Contains(out, "\nsudo:"):
		kind = ErrSudo
	default:
		kind = ErrCommandFailed
	}
	msg := fmt.Sprintf("%v (exit code %d)", err, code)
	if t := tail(out, outputTailLines); t != "" {
		msg += "\n--- last output ---\n" + t
	}
	return fmt.Errorf("%w: %s", kind, msg)
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// logValue renders a job result as JSON for the log.
func logValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(b)
}
