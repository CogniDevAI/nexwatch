package checks

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// checks/check_results collections the scheduler relies on.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

type checkOpts struct {
	name               string
	checkType          string
	target             string
	method             string
	expectedStatus     int
	expectedBody       string
	verifyTLS          bool
	failuresBeforeDown int
	timeoutSeconds     int
	enabled            bool
}

func createCheck(t *testing.T, app core.App, opts checkOpts) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("checks")
	if err != nil {
		t.Fatalf("find checks collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", opts.name)
	rec.Set("type", opts.checkType)
	rec.Set("target", opts.target)
	if opts.method != "" {
		rec.Set("method", opts.method)
	}
	if opts.expectedStatus != 0 {
		rec.Set("expected_status", opts.expectedStatus)
	}
	if opts.expectedBody != "" {
		rec.Set("expected_body_contains", opts.expectedBody)
	}
	rec.Set("verify_tls", opts.verifyTLS)
	if opts.failuresBeforeDown != 0 {
		rec.Set("failures_before_down", opts.failuresBeforeDown)
	}
	if opts.timeoutSeconds != 0 {
		rec.Set("timeout_seconds", opts.timeoutSeconds)
	}
	rec.Set("enabled", opts.enabled)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save check %s: %v", opts.name, err)
	}
	return rec
}

func TestScheduler_RunOnce_HTTPSuccess(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	check := createCheck(t, app, checkOpts{
		name: "http-ok", checkType: "http", target: srv.URL,
		expectedStatus: 200, expectedBody: "hello",
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := record.GetString("status"); got != "up" {
		t.Errorf("status = %q, want up", got)
	}
	if got := record.GetInt("status_code"); got != 200 {
		t.Errorf("status_code = %d, want 200", got)
	}
	if record.GetString("error") != "" {
		t.Errorf("error = %q, want empty", record.GetString("error"))
	}

	snap := s.Snapshot()[check.Id]
	if snap.Status != "up" {
		t.Errorf("snapshot status = %q, want up", snap.Status)
	}
}

func TestScheduler_RunOnce_HTTPUnexpectedStatus(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	check := createCheck(t, app, checkOpts{
		name: "http-500", checkType: "http", target: srv.URL, expectedStatus: 200,
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// A single failed attempt stays "up" during the failures_before_down
	// (default 2) grace period — see nextDebouncedState.
	if got := record.GetString("status"); got != "up" {
		t.Errorf("status = %q, want up (grace period)", got)
	}
	if got := record.GetInt("status_code"); got != 500 {
		t.Errorf("status_code = %d, want 500", got)
	}
	if record.GetString("error") == "" {
		t.Error("expected an unexpected-status-code error message")
	}
}

func TestScheduler_RunOnce_HTTPBodyContainsFails(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("goodbye"))
	}))
	defer srv.Close()

	check := createCheck(t, app, checkOpts{
		name: "http-body", checkType: "http", target: srv.URL,
		expectedStatus: 200, expectedBody: "hello",
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if record.GetString("error") == "" {
		t.Error("expected a body-contains error message")
	}
}

func TestScheduler_RunOnce_HTTPTimeout(t *testing.T) {
	app := newTestApp(t)
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked // never respond within the test's timeout
	}))
	defer func() {
		close(blocked)
		srv.Close()
	}()

	check := createCheck(t, app, checkOpts{
		name: "http-timeout", checkType: "http", target: srv.URL,
		expectedStatus: 200, timeoutSeconds: 1,
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if record.GetString("error") == "" {
		t.Error("expected a timeout error")
	}
}

func TestScheduler_RunOnce_HTTPRecordsTLSExpiry(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := createCheck(t, app, checkOpts{
		name: "https-ok", checkType: "http", target: srv.URL,
		expectedStatus: 200, verifyTLS: false, // self-signed cert from httptest
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if record.GetString("status") != "up" {
		t.Fatalf("status = %q, want up", record.GetString("status"))
	}
	if record.GetDateTime("tls_expires_at").Time().IsZero() {
		t.Error("expected tls_expires_at to be recorded even with verify_tls=false")
	}
}

func TestScheduler_RunOnce_TCP(t *testing.T) {
	app := newTestApp(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	check := createCheck(t, app, checkOpts{
		name: "tcp-ok", checkType: "tcp", target: listener.Addr().String(),
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := record.GetString("status"); got != "up" {
		t.Errorf("status = %q, want up", got)
	}
}

func TestScheduler_RunOnce_TCPClosedPort(t *testing.T) {
	app := newTestApp(t)

	// Bind and immediately close to get a port that's very likely closed.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	check := createCheck(t, app, checkOpts{
		name: "tcp-closed", checkType: "tcp", target: addr, timeoutSeconds: 1,
	})

	s := NewScheduler(app)
	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := record.GetString("status"); got != "up" {
		// A single failed attempt with the default failures_before_down (2)
		// stays "up" during the grace period — see nextDebouncedState.
		t.Errorf("status = %q, want up (grace period)", got)
	}
	if record.GetString("error") == "" {
		t.Error("expected a connection error")
	}
}

type fakePingRunner struct {
	err error
}

func (f fakePingRunner) Ping(target string, timeout time.Duration) error {
	return f.err
}

func TestScheduler_RunOnce_ICMPFakeRunner(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, checkOpts{
		name: "icmp-ok", checkType: "icmp", target: "127.0.0.1",
	})

	s := NewScheduler(app)
	s.SetPingRunner(fakePingRunner{err: nil})

	record, err := s.RunOnce(check.Id)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := record.GetString("status"); got != "up" {
		t.Errorf("status = %q, want up", got)
	}
}

func TestScheduler_DebounceAcrossRuns(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	check := createCheck(t, app, checkOpts{
		name: "flaky", checkType: "http", target: srv.URL,
		expectedStatus: 200, failuresBeforeDown: 2,
	})

	s := NewScheduler(app)

	want := []string{"up", "down", "down"}
	for i, w := range want {
		record, err := s.RunOnce(check.Id)
		if err != nil {
			t.Fatalf("RunOnce #%d: %v", i, err)
		}
		if got := record.GetString("status"); got != w {
			t.Errorf("run #%d: status = %q, want %q", i, got, w)
		}
	}
}

func TestScheduler_ReloadsRunnersOnCollectionChanges(t *testing.T) {
	app := newTestApp(t)
	s := NewScheduler(app)
	s.Start()
	defer s.Stop()

	check := createCheck(t, app, checkOpts{
		name: "reload-me", checkType: "tcp", target: "127.0.0.1:1", enabled: true,
	})

	s.mu.Lock()
	_, started := s.cancels[check.Id]
	s.mu.Unlock()
	if !started {
		t.Fatal("expected a runner to start after creating an enabled check")
	}

	check.Set("enabled", false)
	if err := app.Save(check); err != nil {
		t.Fatalf("disable check: %v", err)
	}
	s.mu.Lock()
	_, stillRunning := s.cancels[check.Id]
	s.mu.Unlock()
	if stillRunning {
		t.Error("expected the runner to stop after disabling the check")
	}

	check.Set("enabled", true)
	if err := app.Save(check); err != nil {
		t.Fatalf("re-enable check: %v", err)
	}
	s.mu.Lock()
	_, restarted := s.cancels[check.Id]
	s.mu.Unlock()
	if !restarted {
		t.Error("expected the runner to restart after re-enabling the check")
	}

	if err := app.Delete(check); err != nil {
		t.Fatalf("delete check: %v", err)
	}
	s.mu.Lock()
	_, afterDelete := s.cancels[check.Id]
	s.mu.Unlock()
	if afterDelete {
		t.Error("expected the runner to stop after deleting the check")
	}
}
