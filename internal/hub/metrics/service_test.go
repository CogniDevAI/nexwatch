package metrics

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// metrics/docker_containers/agents collections this package relies on.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
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

func createAgent(t *testing.T, app core.App, hostname string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent %s: %v", hostname, err)
	}
	return rec
}

func findMetrics(t *testing.T, app core.App, agentID, metricType string) []*core.Record {
	t.Helper()
	recs, err := app.FindRecordsByFilter(
		"metrics",
		"agent_id = {:a} && type = {:t}",
		"-timestamp",
		0,
		0,
		map[string]any{"a": agentID, "t": metricType},
	)
	if err != nil {
		t.Fatalf("find metrics: %v", err)
	}
	return recs
}

func TestIngestMetrics_StoresRawRecordWithCorrectTypeAndResolution(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-ingest")
	svc := NewService(app)

	payload := &protocol.MetricsPayload{
		AgentID: agent.Id,
		Metrics: []protocol.MetricData{
			{Type: "cpu", Data: map[string]any{"total_percent": 42.5}, Timestamp: 1_700_000_000_000},
			{Type: "memory", Data: map[string]any{"used_percent": 60.0}, Timestamp: 1_700_000_001_000},
		},
	}

	svc.IngestMetrics(app, agent.Id, payload)

	cpuRecs := findMetrics(t, app, agent.Id, "cpu")
	if len(cpuRecs) != 1 {
		t.Fatalf("len(cpu records) = %d, want 1", len(cpuRecs))
	}
	if got := cpuRecs[0].GetString("resolution"); got != "raw" {
		t.Errorf("cpu record resolution = %q, want raw", got)
	}
	if got := cpuRecs[0].GetString("agent_id"); got != agent.Id {
		t.Errorf("cpu record agent_id = %q, want %q", got, agent.Id)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(cpuRecs[0].GetString("data")), &data); err != nil {
		t.Fatalf("unmarshal stored data: %v", err)
	}
	if got, _ := data["total_percent"].(float64); got != 42.5 {
		t.Errorf("stored cpu data total_percent = %v, want 42.5", data["total_percent"])
	}

	wantTS := time.UnixMilli(1_700_000_000_000).UTC().Format("2006-01-02 15:04:05.000Z")
	if got := cpuRecs[0].GetDateTime("timestamp").String(); got[:19] != wantTS[:19] {
		t.Errorf("cpu record timestamp = %q, want prefix of %q", got, wantTS)
	}

	memRecs := findMetrics(t, app, agent.Id, "memory")
	if len(memRecs) != 1 {
		t.Fatalf("len(memory records) = %d, want 1", len(memRecs))
	}
}

func TestIngestMetrics_ZeroTimestampFallsBackToNow(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-notimestamp")
	svc := NewService(app)

	before := time.Now().Add(-2 * time.Second)
	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{
		AgentID: agent.Id,
		Metrics: []protocol.MetricData{{Type: "cpu", Data: map[string]any{"total_percent": 1.0}, Timestamp: 0}},
	})
	after := time.Now().Add(2 * time.Second)

	recs := findMetrics(t, app, agent.Id, "cpu")
	if len(recs) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(recs))
	}
	ts := recs[0].GetDateTime("timestamp").Time()
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp for a zero-timestamp metric = %v, want between %v and %v", ts, before, after)
	}
}

func TestIngestMetrics_DockerTypeUpsertsContainer(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-docker")
	svc := NewService(app)

	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{
		AgentID: agent.Id,
		Metrics: []protocol.MetricData{{
			Type: "docker",
			// Shaped like the real docker collector's output
			// (internal/agent/collector/docker.go): one metric per
			// collection cycle, holding an array of per-container maps
			// under "containers" — not a single container's fields at
			// the top level.
			Data: map[string]any{
				"available":       true,
				"container_count": 1,
				"containers": []any{
					map[string]any{
						"container_id": "abc123",
						"name":         "web",
						"image":        "nginx:latest",
						"status":       "running",
						"cpu_percent":  12.5,
						"memory_usage": 1024.0,
						"memory_limit": 2048.0,
						"network_rx":   500.0,
						"network_tx":   250.0,
					},
				},
			},
			Timestamp: time.Now().UnixMilli(),
		}},
	})

	containers, err := app.FindRecordsByFilter(
		"docker_containers",
		"agent_id = {:a} && container_id = {:c}",
		"",
		0, 0,
		map[string]any{"a": agent.Id, "c": "abc123"},
	)
	if err != nil {
		t.Fatalf("find docker_containers: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("len(docker_containers) = %d, want 1", len(containers))
	}
	c := containers[0]
	if got := c.GetString("name"); got != "web" {
		t.Errorf("container name = %q, want web", got)
	}
	if got := c.GetString("image"); got != "nginx:latest" {
		t.Errorf("container image = %q, want nginx:latest", got)
	}
	if got := c.GetFloat("cpu_percent"); got != 12.5 {
		t.Errorf("container cpu_percent = %v, want 12.5", got)
	}
}

func TestIngestMetrics_DockerUpsertUpdatesExistingContainerInPlace(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-docker-update")
	svc := NewService(app)

	send := func(cpuPct float64) {
		svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{
			AgentID: agent.Id,
			Metrics: []protocol.MetricData{{
				Type: "docker",
				Data: map[string]any{
					"containers": []any{
						map[string]any{
							"container_id": "same-container",
							"name":         "web",
							"cpu_percent":  cpuPct,
						},
					},
				},
				Timestamp: time.Now().UnixMilli(),
			}},
		})
	}

	send(10.0)
	send(20.0)

	containers, err := app.FindRecordsByFilter(
		"docker_containers",
		"agent_id = {:a} && container_id = {:c}",
		"",
		0, 0,
		map[string]any{"a": agent.Id, "c": "same-container"},
	)
	if err != nil {
		t.Fatalf("find docker_containers: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("len(docker_containers) = %d, want 1 (upsert must not duplicate)", len(containers))
	}
	if got := containers[0].GetFloat("cpu_percent"); got != 20.0 {
		t.Errorf("container cpu_percent after second ingest = %v, want 20.0 (must update in place)", got)
	}
}

func TestIngestMetrics_DockerWithoutContainerIDIsSkipped(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-docker-nocid")
	svc := NewService(app)

	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{
		AgentID: agent.Id,
		Metrics: []protocol.MetricData{{
			Type: "docker",
			Data: map[string]any{
				"containers": []any{
					map[string]any{"name": "web"}, // no container_id
				},
			},
			Timestamp: time.Now().UnixMilli(),
		}},
	})

	containers, err := app.FindRecordsByFilter("docker_containers", "agent_id = {:a}", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find docker_containers: %v", err)
	}
	if len(containers) != 0 {
		t.Fatalf("len(docker_containers) = %d, want 0 when container_id is missing", len(containers))
	}
}

// TestIngestMetrics_DockerUpsertsEachContainerInOneCycle is the regression
// test for a real bug found via a live agent+hub run: IngestMetrics used to
// pass the whole docker collector payload (m.Data, shaped
// {available, container_count, containers: [...]}) directly to
// upsertDockerContainer, which looks for a "container_id" key that only
// ever exists one level deeper, inside each "containers" entry — so
// docker_containers was never actually populated for any container, ever.
// A host reporting multiple containers in a single collection cycle must
// produce one docker_containers row per container.
func TestIngestMetrics_DockerUpsertsEachContainerInOneCycle(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-docker-multi")
	svc := NewService(app)

	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{
		AgentID: agent.Id,
		Metrics: []protocol.MetricData{{
			Type: "docker",
			Data: map[string]any{
				"available":       true,
				"container_count": 2,
				"containers": []any{
					map[string]any{"container_id": "container-one", "name": "web"},
					map[string]any{"container_id": "container-two", "name": "db"},
				},
			},
			Timestamp: time.Now().UnixMilli(),
		}},
	})

	containers, err := app.FindRecordsByFilter(
		"docker_containers", "agent_id = {:a}", "+container_id", 0, 0,
		map[string]any{"a": agent.Id},
	)
	if err != nil {
		t.Fatalf("find docker_containers: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("len(docker_containers) = %d, want 2 (one per reported container)", len(containers))
	}
	if got := containers[0].GetString("container_id"); got != "container-one" {
		t.Errorf("containers[0].container_id = %q, want container-one", got)
	}
	if got := containers[1].GetString("container_id"); got != "container-two" {
		t.Errorf("containers[1].container_id = %q, want container-two", got)
	}
}

// TestIngestMetrics_DockerUnavailableReportsNoContainers covers the
// docker-not-installed/daemon-unreachable shape the collector reports
// ({available: false, containers: []}) — it must not panic or error.
func TestIngestMetrics_DockerUnavailableReportsNoContainers(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-docker-unavailable")
	svc := NewService(app)

	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{
		AgentID: agent.Id,
		Metrics: []protocol.MetricData{{
			Type:      "docker",
			Data:      map[string]any{"available": false, "containers": []any{}},
			Timestamp: time.Now().UnixMilli(),
		}},
	})

	containers, err := app.FindRecordsByFilter("docker_containers", "agent_id = {:a}", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find docker_containers: %v", err)
	}
	if len(containers) != 0 {
		t.Fatalf("len(docker_containers) = %d, want 0 when docker is unavailable", len(containers))
	}
}

func TestQueryMetrics_FiltersByAgentTypeAndTimeRange(t *testing.T) {
	app := newTestApp(t)
	agent1 := createAgent(t, app, "host-q1")
	agent2 := createAgent(t, app, "host-q2")
	svc := NewService(app)

	now := time.Now()
	svc.IngestMetrics(app, agent1.Id, &protocol.MetricsPayload{Metrics: []protocol.MetricData{
		{Type: "cpu", Data: map[string]any{"total_percent": 1.0}, Timestamp: now.Add(-1 * time.Hour).UnixMilli()},
	}})
	svc.IngestMetrics(app, agent1.Id, &protocol.MetricsPayload{Metrics: []protocol.MetricData{
		{Type: "memory", Data: map[string]any{"used_percent": 2.0}, Timestamp: now.UnixMilli()},
	}})
	svc.IngestMetrics(app, agent2.Id, &protocol.MetricsPayload{Metrics: []protocol.MetricData{
		{Type: "cpu", Data: map[string]any{"total_percent": 3.0}, Timestamp: now.UnixMilli()},
	}})

	results, err := svc.QueryMetrics(agent1.Id, "cpu", now.Add(-2*time.Hour), now.Add(2*time.Hour), "raw")
	if err != nil {
		t.Fatalf("QueryMetrics() unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("QueryMetrics(agent1, cpu) len = %d, want 1", len(results))
	}
	if results[0].GetString("agent_id") != agent1.Id || results[0].GetString("type") != "cpu" {
		t.Errorf("QueryMetrics() returned unexpected record: agent_id=%q type=%q", results[0].GetString("agent_id"), results[0].GetString("type"))
	}
}

func TestGetLatestMetricsByAgent_ReturnsMostRecentPerType(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-latest")
	svc := NewService(app)

	now := time.Now()
	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{Metrics: []protocol.MetricData{
		{Type: "cpu", Data: map[string]any{"total_percent": 10.0}, Timestamp: now.Add(-1 * time.Minute).UnixMilli()},
	}})
	svc.IngestMetrics(app, agent.Id, &protocol.MetricsPayload{Metrics: []protocol.MetricData{
		{Type: "cpu", Data: map[string]any{"total_percent": 90.0}, Timestamp: now.UnixMilli()},
	}})

	latest, err := svc.GetLatestMetricsByAgent(agent.Id)
	if err != nil {
		t.Fatalf("GetLatestMetricsByAgent() unexpected error: %v", err)
	}
	cpuRec, ok := latest["cpu"]
	if !ok {
		t.Fatal("GetLatestMetricsByAgent() missing cpu entry")
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(cpuRec.GetString("data")), &data); err != nil {
		t.Fatalf("unmarshal latest cpu data: %v", err)
	}
	if got, _ := data["total_percent"].(float64); got != 90.0 {
		t.Errorf("GetLatestMetricsByAgent() cpu total_percent = %v, want 90.0 (the most recent sample)", got)
	}
}
