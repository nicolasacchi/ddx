# ddx

CLI for the [Datadog](https://www.datadoghq.com/) API. 35 command groups covering logs, metrics, traces, monitors, incidents, error tracking, RUM, APM, **continuous profiler**, notebooks, and more — with SQL-style log analysis (HAVING, DATE_TRUNC, multiple aggregates), multi-metric formulas, APM latency percentiles (p50/p75/p99), profiler flame-graph aggregation, and parallel health snapshots. Pairs with the [Datadog MCP server](https://docs.datadoghq.com/bits_ai/mcp_server/) for SQL JOINs and trace waterfall views.

## Install

```bash
go install github.com/nicolasacchi/ddx/cmd/ddx@latest
```

Or build from source:

```bash
git clone https://github.com/nicolasacchi/ddx.git
cd ddx
make install
```

## Quick Start

```bash
# Configure credentials
export DD_API_KEY=your-api-key
export DD_APP_KEY=your-app-key
export DD_SITE=datadoghq.eu    # or datadoghq.com

# Or use config file
ddx config add production --api-key KEY --app-key KEY --site datadoghq.eu

# Search error logs
ddx logs search --query "status:error" --from 1h

# SQL-style log analysis
ddx logs sql "SELECT service, COUNT(*) FROM logs WHERE status = 'error' GROUP BY service LIMIT 10" --from 1h

# List alerting monitors
ddx monitors search --query "status:alert"

# Multi-metric query with formulas
ddx metrics query --queries "avg:system.cpu.user{*}" --from 4h

# Error tracking
ddx error-tracking issues search --from 1d

# Health snapshot (parallel fetch)
ddx overview --from 1h
```

## Authentication

Credential resolution order:

1. `--api-key` / `--app-key` / `--site` flags
2. `DD_API_KEY` / `DD_APP_KEY` / `DD_SITE` environment variables
3. Config file (`~/.config/ddx/config.toml`)

### Multi-Project Config

```toml
default_project = "production"

[projects.production]
api_key = "your-api-key"
app_key = "your-app-key"
site = "datadoghq.eu"

[projects.staging]
api_key = "staging-key"
app_key = "staging-app-key"
site = "datadoghq.eu"
```

Switch projects: `ddx --project staging monitors list`

### Required App Key Scopes

`logs_read_data`, `metrics_read`, `monitors_read`, `dashboards_read`, `incidents_read`, `rum_read`, `hosts_read`, `timeseries_query`

## Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--api-key` | | | Datadog API key |
| `--app-key` | | | Datadog App key |
| `--site` | | `datadoghq.eu` | Datadog site |
| `--project` | | | Named project from config |
| `--json` | | false | Force JSON output |
| `--jq` | | | gjson path filter |
| `--from` | | `1h` | Time range start |
| `--to` | | `now` | Time range end |
| `--limit` | | 50 | Max results |
| `--verbose` | `-v` | false | Print request/response to stderr |
| `--quiet` | `-q` | false | Suppress non-error output |

## Output

- **TTY** (interactive terminal): human-readable tables
- **Piped** (non-TTY): JSON automatically
- **`--json`**: force JSON in any context
- **`--jq`**: gjson filter (uses [gjson syntax](https://github.com/tidwall/gjson), NOT jq)

Examples:
```bash
ddx monitors list                              # Table in terminal
ddx monitors list --json                       # Force JSON
ddx monitors list | jq .                       # Auto-JSON when piped
ddx monitors list --jq '#.{id:id,name:name}'   # gjson filter
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | API or network error |
| 2 | Authentication error (401/403) |
| 4 | Not found (404) |

## Commands

### `logs` — Log Search & Analysis

```bash
ddx logs search --query "status:error" --from 1h
ddx logs search --query "service:web kube_namespace:backend-prod" --from 4h --limit 20
ddx logs aggregate --query "status:error" --compute "count" --group-by "service" --from 1h
ddx logs aggregate --query "service:web" --compute "avg(@duration)" --group-by "@http.status_code" --from 4h
ddx logs sql "SELECT service, COUNT(*) FROM logs WHERE status = 'error' GROUP BY service LIMIT 10" --from 1h
ddx logs sql "SELECT @http.status_code, AVG(@duration) FROM logs GROUP BY @http.status_code" --from 4h
```

The `sql` command parses SQL and translates it to the Datadog aggregate API. Supports `COUNT`, `AVG`, `SUM`, `MIN`, `MAX`, `WHERE`, `GROUP BY`, `ORDER BY`, `LIMIT`, `IN`.

### `monitors` — Monitor Management

```bash
ddx monitors list                                    # All monitors
ddx monitors list --status alert                     # Filter by status
ddx monitors get 12345678                            # Single monitor
ddx monitors search --query "status:alert"           # Rich search
ddx monitors search --query "notification:slack-ops" # By notification channel
ddx monitors mute 12345678                           # Mute
ddx monitors unmute 12345678                         # Unmute
```

### `metrics` — Metric Queries & Formulas

```bash
ddx metrics query --queries "avg:system.cpu.user{*}" --from 1h
ddx metrics query --queries "avg:cpu{*}","avg:mem{*}" --formulas "query0 + query1" --from 4h
ddx metrics query --queries "avg:trace.hits{*}" --formulas 'anomalies(query0, "basic", 2)' --from 24h
ddx metrics list --name-filter "system.cpu"
ddx metrics metadata system.cpu.user
ddx metrics context system.cpu.user --include-tags --include-assets
ddx metrics submit --metric custom.gauge --value 42 --tags "env:prod"
```

### `incidents` — Incident Management

```bash
ddx incidents list                                           # Active incidents
ddx incidents list --query "state:active severity:SEV-1"     # Filtered
ddx incidents get INCIDENT_ID --timeline                     # With timeline
ddx incidents facets --query "state:active"                  # Faceted counts
```

### `error-tracking` — Error Tracking

```bash
ddx error-tracking issues search --from 1d
ddx error-tracking issues search --query "service:web" --persona backend --from 7d
ddx error-tracking issues get ISSUE_UUID
```

### `dashboards` — Dashboard Discovery

```bash
ddx dashboards list
ddx dashboards get DASHBOARD_ID
ddx dashboards search --query "backend"
```

### `rum` — Real User Monitoring

```bash
ddx rum apps                                                          # List apps
ddx rum events --query "@type:error" --from 1h                        # Error events
ddx rum events --query "@type:view @view.loading_time:>5000" --from 24h  # Slow pages
ddx rum sessions --from 1h
```

### `traces` — APM Traces & Spans

```bash
ddx traces search --query "service:web status:error" --from 1h
ddx traces get TRACE_ID
ddx traces list --service web --from 1h
```

### `profile` — Continuous Profiler

```bash
# List individual profile uploads (id, pod, version, size, duration)
ddx profile list --service web-1000farmacie --query "kube_deployment:web-canary" --from 1h --limit 20

# Top-N endpoints by metric — the headline view
ddx profile aggregate --service web-1000farmacie --query "kube_deployment:web-canary" \
  --type alloc-samples --by endpoint --top 20 --from 7d

# Top-N hot functions (flame graph leaves) — drills past endpoint into call sites
ddx profile aggregate --service web-1000farmacie --type cpu-time --by function --top 30 --from 1h

# Window totals across all profile types (cpu, alloc, heap, wall)
ddx profile summary --service web-1000farmacie --from 1h

# Per-endpoint delta between two image versions — regression hunting
ddx profile diff --service web-1000farmacie --type alloc-samples \
  --before-version v2026.4.57 --after-version v2026.4.58 --from 2d --top 20

# Per-endpoint delta between two arbitrary scopes — pod-vs-pod, canary-vs-prod, etc.
ddx profile diff --service web-1000farmacie --type alloc-samples \
  --before-query "kube_deployment:web-canary" \
  --after-query  "kube_deployment:worker-canary" --from 1h --top 10

# Single-profile drill-down — pick a profile from `list`, drill in for full metadata
# (profileStart/End, host, all tags, Ruby GC stats: heap_live_slots, minor/major_gc_count,
# total_allocated_objects, allocation sampling stats, profiler settings)
ddx profile get --event-id "<id from list>" --profile-id "<profile-id from list>" --by info

# Per-profile flame leaves (what's hot in this specific 60s sample?)
ddx profile get --event-id E --profile-id P --by function --type cpu-time --top 20
```

Hits `POST /profiling/api/v1/aggregate` (the same endpoint the Datadog UI uses to render the flame graph) and `POST /api/unstable/profiles/list`. Returns server-aggregated JSON — no raw pprof download needed.

**Valid `--type` values** (Ruby): `cpu-time`, `wall-time`, `alloc-samples`, `heap-live-samples`, `heap-live-size`. Note: `alloc-bytes` is **not** supported by the Ruby profiler — it emits allocation count, not byte size. The CLI catches this pre-flight with a clear error.

**`--limit`** sets how many profile uploads the API aggregates server-side (default 50). More profiles → more representative aggregation but slower response. `--top` is independent and trims the displayed results.

**Known limitations**: the Datadog Profiler API does NOT support endpoint-scoped flame graphs (`--by function` is always cross-endpoint; verified against the UI which has the same limitation). Heap-live samples have no endpoint attribution in Ruby — `--type heap-live-samples --by endpoint` returns 100 % `_UNASSIGNED_` and the CLI emits a stderr hint suggesting `--by function` instead.

### `services` — Service Catalog & Dependencies

```bash
ddx services list
ddx services get web-1000farmacie
ddx services deps web-1000farmacie --direction downstream
ddx services deps web-1000farmacie --direction upstream --mermaid
ddx services team backend
```

### `notebooks` — Notebook Management

```bash
ddx notebooks list
ddx notebooks get 12345
ddx notebooks search --query "investigation"
ddx notebooks create --name "CPU Investigation" --cells '[{"type":"markdown","data":"# Summary"}]' --type investigation
ddx notebooks edit 12345 --cells '[...]' --append
ddx notebooks delete 12345
```

### `hosts` — Infrastructure

```bash
ddx hosts list
ddx hosts list --filter "prod"
```

### `slos` — Service Level Objectives

```bash
ddx slos list
ddx slos get SLO_ID
ddx slos history SLO_ID --from 7d
```

### `downtimes` — Maintenance Windows

```bash
ddx downtimes list
ddx downtimes get DOWNTIME_ID
ddx downtimes cancel DOWNTIME_ID
```

### `synthetics` — Synthetic Tests

```bash
ddx synthetics list
ddx synthetics get TEST_ID
```

### `on-call` — On-Call Management

```bash
ddx on-call teams
ddx on-call schedules --team backend
```

### `events` — Event Stream

```bash
ddx events search --query "source:deploy" --from 24h
```

### `audit-logs` — Audit Trail

```bash
ddx audit-logs search --from 24h
ddx audit-logs search --query "@action:modified" --from 7d
```

### `security` — Security Monitoring

```bash
ddx security rules
ddx security signals --query "status:high" --from 24h
```

### `users` — User Management

```bash
ddx users list
ddx users get USER_ID
```

### `tags` — Tag Management

```bash
ddx tags list
ddx tags hosts HOSTNAME
```

### `cicd` — CI/CD Insights

```bash
ddx cicd pipelines
ddx cicd tests
```

### `cloud` — Cloud Integrations

```bash
ddx cloud aws
ddx cloud gcp
ddx cloud azure
```

### `cost` — Cloud Cost Management

```bash
ddx cost query --queries "sum:all.cost{*}.rollup(sum, daily)" --from 7d
ddx cost attribution --from 30d --tags "service,team"
```

### `usage` — Datadog Usage

```bash
ddx usage summary --from 30d
```

### `overview` — Health Snapshot

```bash
ddx overview --from 1h
```

Fetches monitors in alert, active incidents, top error tracking issues, and error log count in parallel.

### `config` — Configuration

```bash
ddx config add production --api-key KEY --app-key KEY --site datadoghq.eu
ddx config remove production
ddx config list
ddx config use production
ddx config current
```

## Building

```bash
make build     # → bin/ddx
make install   # → ~/go/bin/ddx
make test      # Run all tests
make lint      # Run golangci-lint
make clean     # Remove build artifacts
```

## License

MIT
