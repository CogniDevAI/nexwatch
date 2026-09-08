package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// semverPattern accepts "X.Y.Z" or "vX.Y.Z" — permissive on purpose (no
// pre-release/build metadata support) since it only needs to match this
// project's own release tags (.github/workflows/release.yml tags "vX.Y.Z").
var semverPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// updateCommandTimeout bounds how long the hub waits for the agent's
// immediate "started" COMMAND_RESPONSE (or a "failed" one, e.g.
// auto_update_enabled=false) before giving up — it does NOT wait for the
// whole download/verify/install, only for the agent to accept the command.
// Matches dockerActionTimeout's rationale in docker_routes.go.
const updateCommandTimeout = 35 * time.Second

// defaultReleaseBaseURL is the fallback for the "agent_release_base_url"
// setting, matching scripts/install-agent.sh's own default release host.
const defaultReleaseBaseURL = "https://github.com/CogniDevAI/nexwatch/releases/download"

// releaseRepo is this project's GitHub repo, used both by
// defaultReleaseBaseURL above and by githubLatestReleaseFetcher.
const releaseRepo = "CogniDevAI/nexwatch"

// LatestReleaseFetcher fetches the latest published release's version and
// publish time. RegisterUpdateRoutes' production caller
// (cmd/hub/main.go) wires NewGitHubLatestReleaseFetcher; tests inject a
// fake so the route suite never depends on a live network call to
// api.github.com.
type LatestReleaseFetcher interface {
	FetchLatest(ctx context.Context) (version string, publishedAt string, err error)
}

// RegisterUpdateRoutes registers the agent self-update endpoints on
// apiGroup (already bound with the desired auth middleware, mounted at
// "/api/custom"). It returns every route it registered, for
// openapi_test.go to cross-check against docs/openapi.yaml.
func RegisterUpdateRoutes(apiGroup *router.RouterGroup[*core.RequestEvent], broker *commands.Broker, cmdSender CommandSender, fetcher LatestReleaseFetcher) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")
	cache := &latestVersionCache{fetcher: fetcher}

	// POST /api/custom/agents/{id}/update — request a self-update on one
	// connected agent, waiting synchronously for its "started" ack.
	rec.POST("/agents/{id}/update", func(e *core.RequestEvent) error {
		return handleAgentUpdate(e, broker, cmdSender)
	}).Bind(RequireRole(RoleOperator))

	// POST /api/custom/agents/update-all — fan out a self-update request to
	// every connected (status=online) agent, optionally skipping ones
	// already on the target version.
	rec.POST("/agents/update-all", func(e *core.RequestEvent) error {
		return handleAgentUpdateAll(e, broker, cmdSender)
	}).Bind(RequireRole(RoleAdmin))

	// GET /api/custom/agents/latest-version — the latest published release,
	// cached for an hour, never failing the request when GitHub is
	// unreachable (returns {"error": "..."} with 200 instead). The cache
	// lives on this one RegisterUpdateRoutes call (not a package global),
	// so each call — including a fresh one per test scenario — starts
	// cold.
	rec.GET("/agents/latest-version", func(e *core.RequestEvent) error {
		return handleLatestVersion(e, cache)
	})

	return rec.Registered
}

// normalizeVersion validates raw as "X.Y.Z" or "vX.Y.Z" and returns the
// bare "X.Y.Z" form (matching the release asset filename convention),
// stripping any leading "v".
func normalizeVersion(raw string) (string, bool) {
	if !semverPattern.MatchString(raw) {
		return "", false
	}
	return strings.TrimPrefix(raw, "v"), true
}

// dispatchOutcome is the result of attempting to send an "update" COMMAND
// to one agent, carrying the HTTP status a single-agent caller should use.
type dispatchOutcome struct {
	ok     bool
	status int
	errMsg string
}

// dispatchUpdateCommand validates that agent's os/arch are known, sends an
// "update" COMMAND via broker, and waits for the agent's immediate ack.
func dispatchUpdateCommand(ctx context.Context, broker *commands.Broker, sender CommandSender, agent *core.Record, version, baseURL string) dispatchOutcome {
	osName := agent.GetString("platform")
	arch := agent.GetString("arch")
	if osName == "" || arch == "" {
		return dispatchOutcome{
			status: http.StatusBadRequest,
			errMsg: "agent os/arch is not known yet — it must connect at least once with a build that reports them",
		}
	}

	requestID := fmt.Sprintf("upd-%d-%s", time.Now().UnixMilli(), agent.Id[:min(8, len(agent.Id))])
	args := protocol.UpdatePayload{RequestID: requestID, Version: version, BaseURL: baseURL, OS: osName, Arch: arch}
	payload := &protocol.CommandPayload{Command: "update", Args: args.ToArgs()}

	resp, sendErr := broker.Send(ctx, sender, agent.Id, requestID, payload, updateCommandTimeout)
	switch {
	case errors.Is(sendErr, commands.ErrTimeout):
		return dispatchOutcome{status: http.StatusGatewayTimeout, errMsg: "timed out waiting for the agent to acknowledge the update"}
	case sendErr != nil:
		return dispatchOutcome{status: http.StatusBadGateway, errMsg: sendErr.Error()}
	case !resp.OK:
		return dispatchOutcome{status: http.StatusBadGateway, errMsg: resp.Error}
	default:
		return dispatchOutcome{ok: true, status: http.StatusOK}
	}
}

// requestedByString formats the authenticated caller for the
// update_requested_by / audit_log actor fields, the same "<id> (<email>)"
// shape handleRequestThreadDump already uses.
func requestedByString(e *core.RequestEvent) string {
	if e.Auth == nil {
		return ""
	}
	if email := e.Auth.GetString("email"); email != "" {
		return fmt.Sprintf("%s (%s)", e.Auth.Id, email)
	}
	return e.Auth.Id
}

// recordUpdateRequest persists the outcome of one update request onto
// agent's own record — regardless of whether the agent actually
// acknowledged it, so the UI can show "last requested" bookkeeping even
// for a failed dispatch (e.g. the agent was not connected).
func recordUpdateRequest(e *core.RequestEvent, agent *core.Record, version string, outcome dispatchOutcome) {
	agent.Set("update_target_version", version)
	agent.Set("update_requested_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	agent.Set("update_requested_by", requestedByString(e))
	if outcome.ok {
		agent.Set("update_status", "started")
		agent.Set("update_error", "")
	} else {
		agent.Set("update_status", "failed")
		agent.Set("update_error", outcome.errMsg)
	}
	_ = e.App.Save(agent)
}

// handleAgentUpdate handles POST /api/custom/agents/{id}/update.
func handleAgentUpdate(e *core.RequestEvent, broker *commands.Broker, sender CommandSender) error {
	agentID := e.Request.PathValue("id")

	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	version, ok := normalizeVersion(body.Version)
	if !ok {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "version must look like X.Y.Z or vX.Y.Z"})
	}

	agent, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	baseURL := settingString(e.App, "agent_release_base_url", defaultReleaseBaseURL)
	outcome := dispatchUpdateCommand(e.Request.Context(), broker, sender, agent, version, baseURL)
	recordUpdateRequest(e, agent, version, outcome)

	auditResult := "success"
	if !outcome.ok {
		auditResult = "failure"
	}
	audit.Record(e.App, e, audit.Entry{
		Action:     "agent.update.request",
		TargetType: "agents",
		TargetID:   agent.Id,
		AgentID:    agent.Id,
		Details:    map[string]any{"hostname": agent.GetString("hostname"), "version": version, "error": outcome.errMsg},
		Result:     auditResult,
	})

	if !outcome.ok {
		return e.JSON(outcome.status, map[string]any{"ok": false, "error": outcome.errMsg})
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// updateAgentResult is one agent's outcome within the update-all fan-out response.
type updateAgentResult struct {
	AgentID  string `json:"agent_id"`
	Hostname string `json:"hostname"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// handleAgentUpdateAll handles POST /api/custom/agents/update-all.
func handleAgentUpdateAll(e *core.RequestEvent, broker *commands.Broker, sender CommandSender) error {
	var body struct {
		Version      string `json:"version"`
		OnlyOutdated bool   `json:"only_outdated"`
	}
	if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	version, ok := normalizeVersion(body.Version)
	if !ok {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "version must look like X.Y.Z or vX.Y.Z"})
	}

	agents, err := e.App.FindRecordsByFilter("agents", "status = 'online'", "hostname", 0, 0, nil)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list connected agents"})
	}

	baseURL := settingString(e.App, "agent_release_base_url", defaultReleaseBaseURL)
	results := make([]updateAgentResult, 0, len(agents))

	for _, agent := range agents {
		if body.OnlyOutdated && agent.GetString("version") == version {
			continue
		}

		outcome := dispatchUpdateCommand(e.Request.Context(), broker, sender, agent, version, baseURL)
		recordUpdateRequest(e, agent, version, outcome)

		auditResult := "success"
		if !outcome.ok {
			auditResult = "failure"
		}
		audit.Record(e.App, e, audit.Entry{
			Action:     "agent.update.request",
			TargetType: "agents",
			TargetID:   agent.Id,
			AgentID:    agent.Id,
			Details:    map[string]any{"hostname": agent.GetString("hostname"), "version": version, "error": outcome.errMsg, "fan_out": true},
			Result:     auditResult,
		})

		results = append(results, updateAgentResult{
			AgentID:  agent.Id,
			Hostname: agent.GetString("hostname"),
			OK:       outcome.ok,
			Error:    outcome.errMsg,
		})
	}

	return e.JSON(http.StatusOK, map[string]any{"version": version, "results": results})
}

// latestVersionCacheTTL is how long a successful GitHub Releases API
// lookup is cached for, per the task's 1h cache requirement.
const latestVersionCacheTTL = time.Hour

// latestVersionCache wraps a LatestReleaseFetcher with a 1h TTL cache. It
// is instantiated once per RegisterUpdateRoutes call (held in that
// closure), not as a package-level global, so independent hub instances —
// and independent test scenarios, each of which calls RegisterUpdateRoutes
// fresh — never share cached state or a fake fetcher across each other.
type latestVersionCache struct {
	mu          sync.Mutex
	fetcher     LatestReleaseFetcher
	version     string
	publishedAt string
	fetchedAt   time.Time
}

// get returns the cached release info when it's still within TTL,
// otherwise calls the underlying fetcher and caches a successful result.
func (c *latestVersionCache) get(ctx context.Context) (version, publishedAt string, cached bool, err error) {
	c.mu.Lock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < latestVersionCacheTTL {
		version, publishedAt = c.version, c.publishedAt
		c.mu.Unlock()
		return version, publishedAt, true, nil
	}
	c.mu.Unlock()

	version, publishedAt, err = c.fetcher.FetchLatest(ctx)
	if err != nil {
		return "", "", false, err
	}

	c.mu.Lock()
	c.version, c.publishedAt, c.fetchedAt = version, publishedAt, time.Now()
	c.mu.Unlock()

	return version, publishedAt, false, nil
}

// handleLatestVersion handles GET /api/custom/agents/latest-version. It
// never turns an upstream failure into a non-200 response — an offline hub
// or a GitHub rate limit must not break the Agents page, just leave the
// "update available" badge unable to compare against the latest release.
func handleLatestVersion(e *core.RequestEvent, cache *latestVersionCache) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	version, publishedAt, cached, err := cache.get(ctx)
	if err != nil {
		return e.JSON(http.StatusOK, map[string]any{"error": err.Error()})
	}

	return e.JSON(http.StatusOK, map[string]any{
		"version":      version,
		"published_at": publishedAt,
		"cached":       cached,
	})
}

// githubLatestReleaseFetcher is the production LatestReleaseFetcher,
// querying the GitHub Releases API for releaseRepo's latest tag.
type githubLatestReleaseFetcher struct {
	client *http.Client
}

// NewGitHubLatestReleaseFetcher returns the production LatestReleaseFetcher.
func NewGitHubLatestReleaseFetcher() LatestReleaseFetcher {
	return &githubLatestReleaseFetcher{client: &http.Client{Timeout: 5 * time.Second}}
}

func (f *githubLatestReleaseFetcher) FetchLatest(ctx context.Context) (string, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", releaseRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github releases API returned status %d", resp.StatusCode)
	}

	var decoded struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", "", err
	}

	return strings.TrimPrefix(decoded.TagName, "v"), decoded.PublishedAt, nil
}
