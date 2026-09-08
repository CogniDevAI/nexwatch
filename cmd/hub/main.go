package main

import (
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
	"github.com/CogniDevAI/nexwatch/internal/hub/alerts"
	"github.com/CogniDevAI/nexwatch/internal/hub/api"
	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/backup"
	"github.com/CogniDevAI/nexwatch/internal/hub/checks"
	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/hub/logging"
	"github.com/CogniDevAI/nexwatch/internal/hub/logs"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify/channels"
	"github.com/CogniDevAI/nexwatch/internal/hub/report"
	"github.com/CogniDevAI/nexwatch/internal/hub/threaddump"
	"github.com/CogniDevAI/nexwatch/internal/hub/update"
	"github.com/CogniDevAI/nexwatch/internal/hub/userbootstrap"
	"github.com/CogniDevAI/nexwatch/internal/hub/ws"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"

	// Register PocketBase migrations.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	httpAddr := flag.String("http", "0.0.0.0:8090", "HTTP server address")
	retentionDays := flag.Int("retention", 30, "Metric data retention in days")
	bootstrapAgentToken := flag.String("bootstrap-agent-token", "", "If set, ensure a 'bootstrap' agent exists with this token's hash on startup (dev/docker-compose convenience). Also settable via NEXWATCH_BOOTSTRAP_AGENT_TOKEN.")
	bootstrapAdmin := flag.String("bootstrap-admin", "", "If set to EMAIL:PASSWORD, ensure a 'users' record with role 'admin' exists for that email on startup (no-op if it already exists). Also settable via NEXWATCH_BOOTSTRAP_ADMIN.")
	logFormat := flag.String("log-format", "", "Log output format: \"text\" (default) or \"json\". Also settable via NEXWATCH_LOG_FORMAT.")
	flag.Parse()

	if *bootstrapAgentToken == "" {
		*bootstrapAgentToken = os.Getenv("NEXWATCH_BOOTSTRAP_AGENT_TOKEN")
	}
	if *bootstrapAdmin == "" {
		*bootstrapAdmin = os.Getenv("NEXWATCH_BOOTSTRAP_ADMIN")
	}

	logger := logging.Setup(logging.ResolveFormat(*logFormat))

	app := pocketbase.New()

	// Register custom routes before serve.
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Request-id + access-log middleware for every route registered on
		// this router (custom API, /healthz, /ws/agent, and the SPA).
		se.Router.BindFunc(logging.RequestIDMiddleware(logger))

		// Unauthenticated liveness/readiness probe, outside the
		// "/api/custom" auth group so load balancers and uptime monitors
		// can reach it without a PocketBase auth token.
		api.RegisterHealthRoute(se, version)

		// Initialize metrics service.
		metricsSvc := metrics.NewService(app)

		// Initialize log ingestion service.
		logsSvc := logs.NewService(app)

		// Initialize WebSocket hub.
		wsHub := ws.NewHub(app)
		wsHub.SetMetricHandler(metricsSvc.IngestMetrics)
		wsHub.SetLogsHandler(logsSvc.IngestLogs)

		// Wire thread dump response handler and the generic command broker
		// (used by the Docker action endpoint) — both consume every
		// COMMAND_RESPONSE the hub receives; the broker ignores anything
		// whose request id it isn't waiting on (e.g. a thread_dump
		// response), and vice versa.
		tdSvc := threaddump.NewService(app)
		cmdBroker := commands.NewBroker()
		updateSvc := update.NewService()
		wsHub.SetCommandResponseHandler(func(app core.App, payload *protocol.CommandResponsePayload) {
			tdSvc.HandleResponse(app, payload)
			updateSvc.HandleResponse(app, payload)
			cmdBroker.HandleResponse(payload)
		})

		// Bind the audit-log hooks for collections mutated straight
		// through PocketBase's default REST CRUD API (alert_rules,
		// notification_channels, silences, checks, users, agent deletion).
		// Custom /api/custom/* mutating routes call audit.Record directly
		// from their own handlers instead.
		audit.RegisterHooks(app)

		wsHub.StartHeartbeatChecker(90 * time.Second)

		// Register WebSocket endpoint. This uses its own agent-token
		// authentication (see ws.Hub.HandleWebSocket) and must stay outside
		// the /api/custom auth group below.
		se.Router.GET("/ws/agent", func(e *core.RequestEvent) error {
			wsHub.HandleWebSocket(e.Response, e.Request)
			return nil
		})

		// Bootstrap a well-known "bootstrap" agent from a fixed token, if
		// configured. Useful for docker-compose/dev setups where the sample
		// agent's token is known ahead of time.
		if err := agenttoken.BootstrapAgent(app, *bootstrapAgentToken); err != nil {
			slog.Error("bootstrap agent token failed", "error", err)
		}

		// Bootstrap an initial "users" admin account, if configured. Useful
		// so a fresh deployment always has a way to log into the dashboard
		// without a separate manual provisioning step.
		if err := userbootstrap.Admin(app, *bootstrapAdmin); err != nil {
			slog.Error("bootstrap admin user failed", "error", err)
		}

		// Default new "users" records to the "viewer" role when none is
		// given. The "role" field is required, but PocketBase's Go SDK has
		// no schema-level default for select fields, so it is defaulted
		// here before validation runs.
		app.OnRecordCreate("users").BindFunc(func(e *core.RecordEvent) error {
			if e.Record.GetString("role") == "" {
				e.Record.Set("role", "viewer")
			}
			return e.Next()
		})

		// All /api/custom/* routes require an authenticated record (any auth
		// collection — superusers today, a future "users" collection later).
		apiGroup := se.Router.Group("/api/custom")
		apiGroup.Bind(apis.RequireAuth())

		// Register custom API routes.
		api.RegisterRoutes(se, apiGroup, metricsSvc, wsHub)
		api.RegisterDockerRoutes(apiGroup, cmdBroker, wsHub)
		api.RegisterUpdateRoutes(apiGroup, cmdBroker, wsHub, api.NewGitHubLatestReleaseFetcher())
		api.RegisterLogsRoutes(apiGroup)

		// Serve React SPA from ./ui/dist if it exists (production builds).
		distPath := "./ui/dist"
		if info, err := os.Stat(distPath); err == nil && info.IsDir() {
			distFS := os.DirFS(distPath)
			fileServer := http.FileServer(http.FS(distFS))
			// Single catch-all route — handles both "/" and "/{path...}"
			se.Router.GET("/{path...}", func(e *core.RequestEvent) error {
				reqPath := strings.TrimPrefix(e.Request.URL.Path, "/")

				// The PWA manifest and service worker (F11) need explicit
				// content types — Go's mime package has no built-in mapping
				// for ".webmanifest" and would otherwise serve it as plain
				// text — plus, for the service worker, "Service-Worker-
				// Allowed: /" so it can control the whole origin regardless
				// of where it is requested from.
				switch reqPath {
				case "manifest.webmanifest":
					e.Response.Header().Set("Content-Type", "application/manifest+json")
				case "sw.js":
					e.Response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
					e.Response.Header().Set("Service-Worker-Allowed", "/")
				}

				// Serve static assets directly if they exist (js, css, images, etc.)
				if reqPath != "" {
					if _, err := fs.Stat(distFS, reqPath); err == nil {
						fileServer.ServeHTTP(e.Response, e.Request)
						return nil
					}
				}
				// SPA fallback — all other routes serve index.html
				e.Request.URL.Path = "/"
				fileServer.ServeHTTP(e.Response, e.Request)
				return nil
			})
			slog.Info("serving UI", "path", distPath)
		} else {
			slog.Info("UI dist not found, skipping static file serving", "path", distPath)
		}

		// Initialize notification service and register channel notifiers.
		notifySvc := notify.NewService(app)
		notifySvc.RegisterNotifier(channels.NewEmailNotifier())
		notifySvc.RegisterNotifier(channels.NewWebhookNotifier())
		notifySvc.RegisterNotifier(channels.NewTelegramNotifier())
		notifySvc.RegisterNotifier(channels.NewDiscordNotifier())
		notifySvc.RegisterNotifier(channels.NewSlackNotifier())
		notifySvc.RegisterNotifier(channels.NewTeamsNotifier())
		notifySvc.RegisterNotifier(channels.NewPagerDutyNotifier())
		notifySvc.RegisterNotifier(channels.NewNtfyNotifier())
		notifySvc.RegisterNotifier(channels.NewGotifyNotifier())
		notifySvc.RegisterNotifier(channels.NewWebPushNotifier(app))

		// Register alert/notification API routes.
		api.RegisterAlertRoutes(apiGroup, notifySvc)

		// Web Push routes (F11 — PWA + Web Push notifications): the VAPID
		// public key and a per-device test-notification endpoint.
		api.RegisterPushRoutes(apiGroup)

		// Initialize and start the black-box checks scheduler (hub-side
		// HTTP/TCP/ICMP monitoring — internal/hub/checks). Start() also
		// begins the hourly check_results retention sweep and binds hooks
		// so a check create/update/delete takes effect immediately.
		checksScheduler := checks.NewScheduler(app)
		checksScheduler.Start()
		api.RegisterChecksRoutes(apiGroup, checksScheduler)

		// Public status page (unauthenticated, disabled by default via the
		// status_page_enabled setting) and Prometheus exposition
		// (unauthenticated but bearer-token gated via the prometheus_token
		// setting) — both registered directly on the top-level router,
		// outside the "/api/custom" auth group, the same way /healthz is.
		api.RegisterPublicStatusRoute(se, checksScheduler)
		api.RegisterPrometheusRoute(se, checksScheduler, metricsSvc)
		api.RegisterPrometheusTokenRoute(apiGroup)

		// Weekly report preview/send routes (admin-only).
		api.RegisterReportRoutes(apiGroup)

		// Initialize and start the alert evaluation engine. Start() seeds
		// its in-memory state from any alerts still marked "firing" from a
		// previous run before launching the evaluation loop, so a restart
		// neither re-fires a duplicate alert nor loses track of an
		// already-open incident.
		alertEngine := alerts.NewEngine(app)
		alertEngine.SetNotifyFunc(notifySvc.Dispatch)
		alertEngine.SetEscalateFunc(notifySvc.DispatchEscalation)
		alertEngine.Start()

		// Start downsampling & retention background jobs.
		downsampler := metrics.NewDownsampler(app, *retentionDays)
		downsampler.Start()

		// Schedule automatic pb_data backups per the "settings" collection
		// (backups_enabled/backup_cron/backup_keep — defaults to daily at
		// 03:00, keeping the 7 most recent).
		backup.Register(app)

		// Schedule the weekly fleet email report per the "settings"
		// collection (report_enabled/report_cron/report_channel_ids/
		// report_period_days — defaults to disabled, Monday 08:00, 7-day
		// period). Unlike backup.Register, this hot-reloads its schedule
		// when those settings change, no restart required.
		report.Register(app)

		slog.Info("nexwatch hub starting", "version", version, "addr", *httpAddr)
		slog.Info("hub configuration", "retention_days", *retentionDays, "heartbeat_timeout", "90s", "alert_engine", "active")

		return se.Next()
	})

	// Only force the "serve" subcommand (with our --http flag) when the
	// caller didn't already ask for a different PocketBase subcommand
	// (e.g. "superuser upsert EMAIL PASS"). Rewriting os.Args
	// unconditionally here made every non-serve subcommand unreachable.
	if shouldForceServe(os.Args) {
		os.Args = append(os.Args[:1], "serve", "--http="+*httpAddr)
	}

	if err := app.Start(); err != nil {
		slog.Error("hub exited with an error", "error", err)
		os.Exit(1)
	}
}

// shouldForceServe reports whether os.Args should be rewritten to force the
// "serve" subcommand. This is the case when no subcommand was given at all
// (bare invocation, or only flags for the "serve" command such as --http),
// but not when the caller explicitly named a different PocketBase
// subcommand (e.g. "superuser", "migrate").
func shouldForceServe(args []string) bool {
	return len(args) < 2 || strings.HasPrefix(args[1], "-")
}
