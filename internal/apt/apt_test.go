package apt

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const simOutput = `Reading package lists...
Building dependency tree...
The following packages have been kept back:
  linux-image-amd64
The following packages will be upgraded:
  curl libcurl4
2 upgraded, 0 newly installed, 0 to remove and 1 not upgraded.
Inst curl [7.88.1-10] (7.88.1-10+deb12u5 Debian:12/stable [amd64])
`

type call struct {
	privileged bool
	args       []string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []call
	// respond returns output, exit code and error for a call.
	respond func(ctx context.Context, privileged bool, args []string) (string, int, error)
}

func (f *fakeRunner) Run(ctx context.Context, privileged bool, args ...string) (string, int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{privileged, args})
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(ctx, privileged, args)
	}
	return simOutput, 0, nil
}

func (f *fakeRunner) count(args []string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if slices.Equal(c.args, args) {
			n++
		}
	}
	return n
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestManager(r Runner, c *clock) *Manager {
	return NewManager(Config{
		Runner:            r,
		UpdateMinInterval: 15 * time.Minute,
		JobTimeout:        time.Minute,
		Now:               c.now,
	})
}

func TestParseSummary(t *testing.T) {
	s, err := ParseSummary(simOutput)
	if err != nil {
		t.Fatal(err)
	}
	want := Summary{Upgraded: 2, NotUpgraded: 1}
	if s != want {
		t.Fatalf("got %+v, want %+v", s, want)
	}
	if _, err := ParseSummary("nothing here"); !errors.Is(err, ErrNoSummary) {
		t.Fatalf("got %v, want ErrNoSummary", err)
	}
}

func TestParsePackages(t *testing.T) {
	out := `The following packages will be upgraded:
  a b
  c
3 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
`
	if got, want := ParsePackages(out, upgradedHeader), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := ParsePackages(simOutput, upgradedHeader); !slices.Equal(got, []string{"curl", "libcurl4"}) {
		t.Fatalf("kept-back packages must not be listed: %q", got)
	}
	if got := ParsePackages("0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", upgradedHeader); got != nil {
		t.Fatalf("got %q, want nil", got)
	}
}

func TestUpdateThrottle(t *testing.T) {
	r := &fakeRunner{}
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := newTestManager(r, c)
	ctx := context.Background()

	res, err := m.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Refreshed || res.Upgradable != 2 || res.KeptBack != 1 {
		t.Fatalf("unexpected first result %+v", res)
	}

	c.t = c.t.Add(5 * time.Minute)
	res, err = m.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Refreshed {
		t.Fatalf("second update within interval should be throttled: %+v", res)
	}
	if !res.LastRefresh.Equal(c.t.Add(-5 * time.Minute)) {
		t.Fatalf("unexpected last refresh %v", res.LastRefresh)
	}

	c.t = c.t.Add(15 * time.Minute)
	res, err = m.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Refreshed {
		t.Fatalf("update after interval should refresh: %+v", res)
	}
	if n := r.count(argsUpdate); n != 2 {
		t.Fatalf("apt-get update ran %d times, want 2", n)
	}
	if n := r.count(argsSimulateUpgrade); n != 3 {
		t.Fatalf("simulation ran %d times, want 3", n)
	}
}

func TestUpdateFailureDoesNotThrottle(t *testing.T) {
	fail := true
	r := &fakeRunner{respond: func(_ context.Context, _ bool, args []string) (string, int, error) {
		if fail && slices.Equal(args, argsUpdate) {
			return "E: network unreachable\n", 100, errors.New("exit status 100")
		}
		return simOutput, 0, nil
	}}
	m := newTestManager(r, &clock{t: time.Now()})
	if _, err := m.Update(context.Background()); !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("got %v, want ErrCommandFailed", err)
	}
	fail = false
	res, err := m.Update(context.Background())
	if err != nil || !res.Refreshed {
		t.Fatalf("retry after failure should refresh: %+v, %v", res, err)
	}
}

func TestUpgradeSync(t *testing.T) {
	r := &fakeRunner{}
	m := newTestManager(r, &clock{t: time.Now()})
	res, err := m.Upgrade(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" || res.Summary == nil || res.Summary.Upgraded != 2 {
		t.Fatalf("unexpected result %+v", res)
	}
	if !slices.Equal(res.Upgraded, []string{"curl", "libcurl4"}) {
		t.Fatalf("unexpected upgraded packages %q", res.Upgraded)
	}
	if !r.calls[0].privileged {
		t.Fatal("upgrade must run privileged")
	}
}

func TestAutoremoveSync(t *testing.T) {
	out := `The following packages will be REMOVED:
  libfoo1 libbar2
  libbaz3
0 upgraded, 0 newly installed, 3 to remove and 0 not upgraded.
`
	r := &fakeRunner{respond: func(context.Context, bool, []string) (string, int, error) { return out, 0, nil }}
	m := newTestManager(r, &clock{t: time.Now()})
	res, err := m.Autoremove(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary == nil || res.Summary.Removed != 3 || res.Upgraded != nil {
		t.Fatalf("unexpected result %+v", res)
	}
	if want := []string{"libfoo1", "libbar2", "libbaz3"}; !slices.Equal(res.Removed, want) {
		t.Fatalf("got removed %q, want %q", res.Removed, want)
	}
}

func TestAsyncAndBusy(t *testing.T) {
	release := make(chan struct{})
	r := &fakeRunner{respond: func(_ context.Context, _ bool, _ []string) (string, int, error) {
		<-release
		return simOutput, 0, nil
	}}
	m := newTestManager(r, &clock{t: time.Now()})
	ctx := context.Background()

	res, err := m.Upgrade(ctx, true)
	if err != nil || res.Status != "started" {
		t.Fatalf("async upgrade: %+v, %v", res, err)
	}
	if _, err := m.Autoremove(ctx, true); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}
	if _, err := m.Update(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}

	close(release)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Autoremove(ctx, false); err != nil {
		t.Fatalf("autoremove after job finished: %v", err)
	}
}

func TestSyncCallerGivesUp(t *testing.T) {
	release := make(chan struct{})
	r := &fakeRunner{respond: func(_ context.Context, _ bool, _ []string) (string, int, error) {
		<-release
		return simOutput, 0, nil
	}}
	m := newTestManager(r, &clock{t: time.Now()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Upgrade(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	// The job keeps running even though the caller left.
	if _, err := m.Upgrade(context.Background(), true); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}
	close(release)
	if err := m.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		out  string
		want error
	}{
		{"E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 123 (apt-get)\n", ErrLocked},
		{"E: dpkg was interrupted, you must manually run 'dpkg --configure -a' to correct the problem.\n", ErrDpkgInterrupted},
		{"sudo: a password is required\n", ErrSudo},
		{"E: Some other failure\n", ErrCommandFailed},
	}
	for _, tc := range cases {
		err := classify(context.Background(), tc.out, 100, errors.New("exit status 100"))
		if !errors.Is(err, tc.want) {
			t.Errorf("classify(%q) = %v, want %v", tc.out, err, tc.want)
		}
		if !strings.Contains(err.Error(), "exit code 100") {
			t.Errorf("error should include exit code: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done()
	if err := classify(ctx, "", -1, ctx.Err()); !errors.Is(err, ErrTimeout) {
		t.Errorf("got %v, want ErrTimeout", err)
	}
}

func TestTail(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		b.WriteString(strings.Repeat("x", i%3) + "\n")
	}
	if got := len(strings.Split(tail(b.String(), 40), "\n")); got > 40 {
		t.Fatalf("tail returned %d lines", got)
	}
}

func TestSudoersRule(t *testing.T) {
	rule := SudoersRule("gummy")
	for _, want := range []string{
		`gummy ALL=(root) NOPASSWD: /usr/bin/apt-get -q update,`,
		`/usr/bin/apt-get -q -o Dpkg\:\:Options\:\:\=--force-confdef -o Dpkg\:\:Options\:\:\=--force-confold upgrade -y`,
		`autoremove -y`,
		`Defaults:gummy env_keep += "DEBIAN_FRONTEND"`,
	} {
		if !strings.Contains(rule, want) {
			t.Errorf("sudoers rule missing %q:\n%s", want, rule)
		}
	}
}

func TestStatus(t *testing.T) {
	release := make(chan struct{})
	fail := false
	r := &fakeRunner{respond: func(_ context.Context, _ bool, args []string) (string, int, error) {
		if slices.Equal(args, argsUpgrade) {
			<-release
		}
		if fail {
			return "E: Some failure\n", 100, errors.New("exit status 100")
		}
		return simOutput, 0, nil
	}}
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := newTestManager(r, c)
	ctx := context.Background()

	st := m.Status()
	if st.Running != nil || len(st.Last) != 0 || !st.LastRefresh.IsZero() || !st.NextRefresh.IsZero() {
		t.Fatalf("unexpected initial status %+v", st)
	}

	if _, err := m.Update(ctx); err != nil {
		t.Fatal(err)
	}
	st = m.Status()
	if !st.LastRefresh.Equal(c.t) || !st.NextRefresh.Equal(c.t.Add(15*time.Minute)) {
		t.Fatalf("unexpected refresh times %+v", st)
	}
	if u := st.Last["update"]; u.State != StateSucceeded || u.Result.(UpdateResult).Upgradable != 2 {
		t.Fatalf("unexpected last update %+v", u)
	}

	if _, err := m.Upgrade(ctx, true); err != nil {
		t.Fatal(err)
	}
	st = m.Status()
	if st.Running == nil || st.Running.Job != "upgrade" || st.Running.State != StateRunning {
		t.Fatalf("expected running upgrade, got %+v", st.Running)
	}
	if _, err := m.Autoremove(ctx, false); !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("busy error should name the running job: %v", err)
	}

	fail = true
	close(release)
	if err := m.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	st = m.Status()
	if st.Running != nil {
		t.Fatalf("nothing should be running: %+v", st.Running)
	}
	u := st.Last["upgrade"]
	if u.State != StateFailed || !strings.Contains(u.Error, "exit code 100") || u.Result != nil || u.FinishedAt.IsZero() {
		t.Fatalf("unexpected last upgrade %+v", u)
	}
	if st.Last["update"].State != StateSucceeded {
		t.Fatal("the update outcome must survive a later upgrade")
	}
}
