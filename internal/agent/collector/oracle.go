package collector

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// OracleCollector gathers Oracle DB metrics via sqlplus (OS auth / as sysdba).
// Requires the agent to run as a user in the dba OS group (e.g. oracle).
type OracleCollector struct {
	oracleHome string // ORACLE_HOME path
	oracleSID  string // ORACLE_SID
}

// NewOracleCollector creates a new Oracle collector.
// oracleHome and oracleSID are read from the agent config.
func NewOracleCollector(oracleHome, oracleSID string) *OracleCollector {
	return &OracleCollector{
		oracleHome: oracleHome,
		oracleSID:  oracleSID,
	}
}

// Name returns the collector identifier.
func (c *OracleCollector) Name() string { return "oracle" }

// Collect runs all Oracle queries and returns the combined result.
func (c *OracleCollector) Collect(ctx context.Context) (map[string]any, error) {
	if c.oracleHome == "" {
		return nil, fmt.Errorf("oracle_home is not configured")
	}
	if c.oracleSID == "" {
		return nil, fmt.Errorf("oracle_sid is not configured")
	}
	log.Printf("[oracle] collecting from SID=%s HOME=%s", c.oracleSID, c.oracleHome)
	script := buildScript()
	out, err := c.runSqlplus(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("sqlplus error: %w", err)
	}
	log.Printf("[oracle] collected %d bytes of output", len(out))
	return parseOutput(out), nil
}

// runSqlplus executes a sqlplus script and returns the raw output.
func (c *OracleCollector) runSqlplus(ctx context.Context, script string) (string, error) {
	sqlplusPath := c.oracleHome + "/bin/sqlplus"
	if _, err := os.Stat(sqlplusPath); err != nil {
		return "", fmt.Errorf("sqlplus not found at %s", sqlplusPath)
	}

	cmd := exec.CommandContext(ctx, sqlplusPath, "-s", "/ as sysdba")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(),
		"ORACLE_HOME="+c.oracleHome,
		"ORACLE_SID="+c.oracleSID,
		"PATH="+c.oracleHome+"/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LD_LIBRARY_PATH="+c.oracleHome+"/lib",
		"NLS_LANG=AMERICAN_AMERICA.AL32UTF8",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		log.Printf("[oracle] sqlplus stderr: %s", stderr.String())
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// buildScript returns the SQL script with all metric queries.
// Each section is delimited by a SECTION: marker for easy parsing.
func buildScript() string {
	return `
SET PAGESIZE 0 FEEDBACK OFF VERIFY OFF HEADING OFF ECHO OFF LINESIZE 4000 TRIMSPOOL ON
SET COLSEP '|'
SET NUMFORMAT 99999999999

PROMPT SECTION:instance
SELECT instance_name||'|'||status||'|'||database_status||'|'||host_name||'|'||
       TO_CHAR(startup_time,'YYYY-MM-DD HH24:MI:SS')||'|'||version
FROM v$instance;

PROMPT SECTION:sessions
SELECT
  COUNT(*)||'|'||
  SUM(CASE WHEN status='ACTIVE' THEN 1 ELSE 0 END)||'|'||
  SUM(CASE WHEN status='INACTIVE' THEN 1 ELSE 0 END)||'|'||
  SUM(CASE WHEN wait_class='Idle' THEN 0 ELSE 1 END)||'|'||
  SUM(CASE WHEN blocking_session IS NOT NULL THEN 1 ELSE 0 END)
FROM v$session
WHERE type='USER';

PROMPT SECTION:blocked_sessions
SELECT s.sid||'|'||s.serial#||'|'||s.username||'|'||s.status||'|'||
       NVL(TO_CHAR(s.blocking_session),'0')||'|'||s.wait_class||'|'||s.event||'|'||s.seconds_in_wait||'|'||
       SUBSTR(NVL(q.sql_text,''),1,200)
FROM v$session s
LEFT JOIN v$sql q ON s.sql_id = q.sql_id AND s.sql_child_number = q.child_number
WHERE s.blocking_session IS NOT NULL AND s.type='USER'
FETCH FIRST 20 ROWS ONLY;

PROMPT SECTION:top_sql
SELECT sql_id||'|'||executions||'|'||
       ROUND(elapsed_time/1000000,2)||'|'||
       ROUND(elapsed_time/NULLIF(executions,0)/1000000,4)||'|'||
       ROUND(cpu_time/1000000,2)||'|'||
       buffer_gets||'|'||disk_reads||'|'||
       SUBSTR(sql_text,1,300)
FROM v$sql
WHERE executions > 0
ORDER BY elapsed_time DESC
FETCH FIRST 15 ROWS ONLY;

PROMPT SECTION:tablespaces
SELECT t.tablespace_name||'|'||
       ROUND(m.used_space*8192/1024/1024,2)||'|'||
       ROUND(m.tablespace_size*8192/1024/1024,2)||'|'||
       ROUND(m.used_percent,2)||'|'||
       t.status||'|'||t.contents
FROM dba_tablespace_usage_metrics m
JOIN dba_tablespaces t ON t.tablespace_name = m.tablespace_name
ORDER BY m.used_percent DESC;

PROMPT SECTION:sga
SELECT name||'|'||ROUND(bytes/1024/1024,2) FROM v$sgainfo ORDER BY name;

PROMPT SECTION:pga
SELECT name||'|'||ROUND(value/1024/1024,2) FROM v$pgastat
WHERE name IN ('total PGA inuse','total PGA allocated','maximum PGA allocated','aggregate PGA target parameter');

PROMPT SECTION:waits
SELECT event||'|'||total_waits||'|'||ROUND(time_waited/100,2)||'|'||wait_class
FROM v$system_event
WHERE wait_class != 'Idle' AND total_waits > 0
ORDER BY time_waited DESC
FETCH FIRST 15 ROWS ONLY;

PROMPT SECTION:locks
SELECT l.sid||'|'||s.username||'|'||l.type||'|'||
       DECODE(l.lmode,0,'None',1,'Null',2,'Row-S',3,'Row-X',4,'Share',5,'S/Row-X',6,'Exclusive')||'|'||
       DECODE(l.request,0,'None',1,'Null',2,'Row-S',3,'Row-X',4,'Share',5,'S/Row-X',6,'Exclusive')||'|'||
       l.ctime||'|'||NVL(o.object_name,'')
FROM v$lock l
JOIN v$session s ON s.sid = l.sid
LEFT JOIN dba_objects o ON o.object_id = l.id1
WHERE l.type IN ('TM','TX') AND s.type='USER'
ORDER BY l.ctime DESC
FETCH FIRST 20 ROWS ONLY;

PROMPT SECTION:redo
SELECT ROUND(NVL(SUM(blocks*block_size),0)/1024/1024,2) FROM v$archived_log
WHERE first_time >= SYSDATE - 1/24 AND standby_dest='NO';

EXIT;
`
}

// parseOutput splits the sqlplus output into sections and parses each one.
func parseOutput(raw string) map[string]any {
	result := map[string]any{}

	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		// PROMPT outputs the text directly — match "SECTION:<name>"
		if strings.HasPrefix(line, "SECTION:") {
			current = strings.TrimPrefix(line, "SECTION:")
			sections[current] = []string{}
			continue
		}
		if current != "" && line != "" && !strings.HasPrefix(line, "ERROR") && !strings.HasPrefix(line, "ORA-") {
			sections[current] = append(sections[current], line)
		}
	}

	// instance
	if rows := sections["instance"]; len(rows) > 0 {
		f := splitRow(rows[0], 6)
		result["instance"] = map[string]any{
			"name":            f[0],
			"status":          f[1],
			"database_status": f[2],
			"host":            f[3],
			"startup_time":    f[4],
			"version":         f[5],
		}
	}

	// sessions summary
	if rows := sections["sessions"]; len(rows) > 0 {
		f := splitRow(rows[0], 5)
		result["sessions"] = map[string]any{
			"total":    toInt(f[0]),
			"active":   toInt(f[1]),
			"inactive": toInt(f[2]),
			"waiting":  toInt(f[3]),
			"blocked":  toInt(f[4]),
		}
	}

	// blocked sessions detail
	blocked := make([]map[string]any, 0)
	for _, row := range sections["blocked_sessions"] {
		f := splitRow(row, 9)
		if f[0] == "" {
			continue
		}
		blocked = append(blocked, map[string]any{
			"sid":             toInt(f[0]),
			"serial":          toInt(f[1]),
			"username":        f[2],
			"status":          f[3],
			"blocking_sid":    toInt(f[4]),
			"wait_class":      f[5],
			"event":           f[6],
			"seconds_in_wait": toInt(f[7]),
			"sql_text":        f[8],
		})
	}
	result["blocked_sessions"] = blocked

	// top SQL
	topSQL := make([]map[string]any, 0)
	for _, row := range sections["top_sql"] {
		f := splitRow(row, 8)
		if f[0] == "" {
			continue
		}
		topSQL = append(topSQL, map[string]any{
			"sql_id":           f[0],
			"executions":       toInt(f[1]),
			"elapsed_secs":     toFloat(f[2]),
			"elapsed_per_exec": toFloat(f[3]),
			"cpu_secs":         toFloat(f[4]),
			"buffer_gets":      toInt(f[5]),
			"disk_reads":       toInt(f[6]),
			"sql_text":         f[7],
		})
	}
	result["top_sql"] = topSQL

	// tablespaces
	tablespaces := make([]map[string]any, 0)
	for _, row := range sections["tablespaces"] {
		f := splitRow(row, 6)
		if f[0] == "" {
			continue
		}
		tablespaces = append(tablespaces, map[string]any{
			"name":     f[0],
			"used_mb":  toFloat(f[1]),
			"total_mb": toFloat(f[2]),
			"used_pct": toFloat(f[3]),
			"status":   f[4],
			"contents": f[5],
		})
	}
	result["tablespaces"] = tablespaces

	// SGA
	sga := map[string]any{}
	for _, row := range sections["sga"] {
		f := splitRow(row, 2)
		if f[0] != "" {
			sga[sanitizeKey(f[0])] = toFloat(f[1])
		}
	}
	result["sga"] = sga

	// PGA
	pga := map[string]any{}
	for _, row := range sections["pga"] {
		f := splitRow(row, 2)
		if f[0] != "" {
			pga[sanitizeKey(f[0])] = toFloat(f[1])
		}
	}
	result["pga"] = pga

	// wait events
	waits := make([]map[string]any, 0)
	for _, row := range sections["waits"] {
		f := splitRow(row, 4)
		if f[0] == "" {
			continue
		}
		waits = append(waits, map[string]any{
			"event":         f[0],
			"total_waits":   toInt(f[1]),
			"time_waited_s": toFloat(f[2]),
			"wait_class":    f[3],
		})
	}
	result["waits"] = waits

	// locks
	locks := make([]map[string]any, 0)
	for _, row := range sections["locks"] {
		f := splitRow(row, 7)
		if f[0] == "" {
			continue
		}
		locks = append(locks, map[string]any{
			"sid":         toInt(f[0]),
			"username":    f[1],
			"lock_type":   f[2],
			"lock_mode":   f[3],
			"request":     f[4],
			"ctime":       toInt(f[5]),
			"object_name": f[6],
		})
	}
	result["locks"] = locks

	// redo
	if rows := sections["redo"]; len(rows) > 0 {
		result["redo_mb_last_hour"] = toFloat(strings.TrimSpace(rows[0]))
	}

	return result
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func splitRow(row string, expected int) []string {
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	for len(parts) < expected {
		parts = append(parts, "")
	}
	return parts
}

func toInt(s string) int {
	s = strings.TrimSpace(s)
	v, _ := strconv.Atoi(s)
	return v
}

func toFloat(s string) float64 {
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func sanitizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
