package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/alerts"
	"github.com/CogniDevAI/nexwatch/internal/hub/api"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify/channels"
	"github.com/CogniDevAI/nexwatch/internal/hub/threaddump"
	"github.com/CogniDevAI/nexwatch/internal/hub/ws"

	// Register PocketBase migrations.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	httpAddr := flag.String("http", "0.0.0.0:8090", "HTTP server address")
	retentionDays := flag.Int("retention", 30, "Metric data retention in days")
	flag.Parse()

	app := pocketbase.New()

	// Register custom routes before serve.
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Initialize metrics service.
		metricsSvc := metrics.NewService(app)

		// Initialize WebSocket hub.
		wsHub := ws.NewHub(app)
		wsHub.SetMetricHandler(metricsSvc.IngestMetrics)

		// Wire thread dump response handler.
		tdSvc := threaddump.NewService(app)
		wsHub.SetCommandResponseHandler(tdSvc.HandleResponse)

		wsHub.StartHeartbeatChecker(90 * time.Second)

		// Register WebSocket endpoint.
		se.Router.GET("/ws/agent", func(e *core.RequestEvent) error {
			wsHub.HandleWebSocket(e.Response, e.Request)
			return nil
		})

		// Register custom API routes.
		api.RegisterRoutes(se, metricsSvc, wsHub)

		// Serve React SPA from ./ui/dist if it exists (production builds).
		distPath := "./ui/dist"
		if info, err := os.Stat(distPath); err == nil && info.IsDir() {
			distFS := os.DirFS(distPath)
			fileServer := http.FileServer(http.FS(distFS))
			// Single catch-all route — handles both "/" and "/{path...}"
			se.Router.GET("/{path...}", func(e *core.RequestEvent) error {
				reqPath := strings.TrimPrefix(e.Request.URL.Path, "/")
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
			log.Printf("Serving UI from %s", distPath)
		} else {
			log.Printf("UI dist not found at %s — skipping static file serving", distPath)
		}

		// Initialize notification service and register channel notifiers.
		notifySvc := notify.NewService(app)
		notifySvc.RegisterNotifier(channels.NewEmailNotifier())
		notifySvc.RegisterNotifier(channels.NewWebhookNotifier())
		notifySvc.RegisterNotifier(channels.NewTelegramNotifier())
		notifySvc.RegisterNotifier(channels.NewDiscordNotifier())

		// Register alert/notification API routes.
		api.RegisterAlertRoutes(se, notifySvc)

		// Initialize and start the alert evaluation engine.
		alertEngine := alerts.NewEngine(app)
		alertEngine.SetNotifyFunc(notifySvc.Dispatch)
		alertEngine.Start()

		// Start downsampling & retention background jobs.
		downsampler := metrics.NewDownsampler(app, *retentionDays)
		downsampler.Start()

		log.Printf("NexWatch Hub %s starting on %s\n", version, *httpAddr)
		log.Printf("Retention: %d days | Heartbeat timeout: 90s | Alert engine: active\n", *retentionDays)

		return se.Next()
	})

	// Set the HTTP address from the flag.
	os.Args = append(os.Args[:1], "serve", "--http="+*httpAddr)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
