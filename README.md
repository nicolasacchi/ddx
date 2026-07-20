# ddx

CLI for the [Datadog](https://www.datadoghq.com/) API. 34 command groups covering logs, metrics, traces, monitors, incidents, error tracking, RUM, APM, **continuous profiler**, usage/cost billing, notebooks, and more — with SQL-style log analysis (HAVING, DATE_TRUNC, multiple aggregates), multi-metric formulas and scalar queries, custom-metric cardinality cost control, APM latency percentiles (p50/p75/p99) and full trace waterfalls, profiler flame-graph aggregation, and parallel health snapshots. Pairs with the [Datadog MCP server](https://docs.datadoghq.com/bits_ai/mcp_server/) for log SQL JOINs — RUM aggregation and trace waterfalls are native now.

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

# Full trace waterfall (indented span tree)
ddx traces waterfall <trace-id>

# Current-month estimated Datadog bill
ddx usage estimated

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

If a `--jq` filter comes back empty against non-empty data (a likely syntax mistake, not an intentionally-empty result), ddx prints a short gjson path cheat-sheet to stderr once per invocation — never to stdout, so it never pollutes JSON/table output.

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

**Log cost-control config** (log-based metrics, archives, custom destinations — the levers for turning expensive indexed-log queries into cheap metrics or routing logs elsewhere):

```bash
ddx logs metrics list
ddx logs metrics create --name logs.bot.count --query "@user_agent.type:crawler" --compute count --yes
ddx logs archives list
ddx logs archives create --name "Nginx Archive" --query "source:nginx" \
  --destination-json '{"type":"s3","bucket":"my-bucket","integration":{"account_id":"123456789012","role_name":"my-role"}}' --yes
ddx logs destinations list
ddx logs destinations create --name "Nginx logs" --query "source:nginx" \
  --forwarder-json '{"type":"http","endpoint":"https://example.com","auth":{"type":"basic","username":"u","password":"p"}}' --yes
```

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

### `metrics` — Metric Queries, Scalar, & Cardinality Control

```bash
ddx metrics query --queries "avg:system.cpu.user{*}" --from 1h
ddx metrics query --queries "avg:cpu{*}","avg:mem{*}" --formulas "query0 + query1" --from 4h
ddx metrics query --queries "avg:trace.hits{*}" --formulas 'anomalies(query0, "basic", 2)' --from 24h
ddx metrics query --metric system.cpu.user --from 1h                       # shortcut for avg:system.cpu.user{*}
ddx metrics query --queries "avg:system.cpu.user{*}" --interval 1h --from 24h  # duration-string interval, not just millis
ddx metrics query --queries "sum:system.disk.used{*}" --strict-types       # error (not just warn) on sum: over a gauge
ddx metrics list --name-filter "system.cpu"
ddx metrics metadata system.cpu.user
ddx metrics context system.cpu.user --include-tags --include-assets
ddx metrics submit --metric custom.gauge --value 42 --tags "env:prod"

# Scalar queries — one aggregated number per group (Query Value/Table/Toplist widgets)
ddx metrics scalar --queries "avg:system.cpu.user{*} by {env}" --from 1h
ddx metrics scalar --queries "sum:trace.web.request.hits{*}","sum:trace.web.request.errors{*}" \
  --formulas "query1 / query0" --reducer sum --from 4h

# Custom-metric cardinality cost control
ddx metrics volumes system.cpu.user --window 7200                          # hourly avg cardinality / point volume
ddx metrics estimate dist.request.latency --groups app,host --pct          # estimated output-series count
ddx metrics tag-cardinalities system.cpu.user                              # per-tag cardinality deltas
ddx metrics tags get http.endpoint.request                                 # current tag configuration
ddx metrics tags set http.endpoint.request --metric-type distribution --tags app,datacenter --yes
ddx metrics tag-rules list                                                 # org-wide tag indexing rules
ddx metrics tag-rules create --name my-rule --metric-name-matches "dd.test.*" --tags env,service --yes
```

`--queries`/`--formulas` are repeatable flags (`--queries "avg:a{*}" --queries "avg:b{*}"`); a single value may also comma-join multiple queries — the split happens outside `{}`/`()`/quotes, so `avg:a{x:1,y:2},avg:b{*}` still works as one `--queries` value.

### `incidents` — Incident Management

```bash
ddx incidents list                                           # Active incidents (first page)
ddx incidents list --all                                     # Every page (page[offset]/page[size], capped 20 pages)
ddx incidents list --query "state:active severity:SEV-1"     # Filtered
ddx incidents get INCIDENT_ID --timeline                     # With timeline
ddx incidents facets --query "state:active"                  # Faceted counts
ddx incidents create --title "Checkout errors spiking" --severity SEV-2 --yes
ddx incidents update 45 --severity SEV-3 --summary "..."     # Patch fields
ddx incidents resolve 45 --root-cause-file ./rca.md          # state=resolved, resolved=now
```

`update` and `resolve` accept either the public id (`45`) or the UUID. When the public id is passed, ddx does a `GET /api/v2/incidents/{id}` first to resolve the UUID the PATCH endpoint requires.

`create` **never sets `fields.slug`/`public_id`** — Datadog assigns the incident number on creation, printed after the call. This matches repo policy: `IR-` numbers belong to Datadog, never invented locally.

### `error-tracking` — Error Tracking

```bash
ddx error-tracking issues search --from 1d
ddx error-tracking issues search --query "service:web" --persona backend --from 7d
ddx error-tracking issues search --persona all --from 1d     # fan out backend+frontend+mobile, merge results
ddx error-tracking issues get ISSUE_UUID
ddx error-tracking issues set-state ISSUE_UUID --state RESOLVED --yes
ddx error-tracking issues assign ISSUE_UUID --assignee user@example.com --yes
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
ddx rum aggregate --query "@type:error" --compute "count" --group-by "@view.url" --from 1h  # server-side grouping

# Retention filters (which RUM events are actually stored, scoped per --app)
ddx rum retention-filters list --app APP_ID
ddx rum retention-filters create --app APP_ID --name "Errors only" --event-type error --sample-rate 100 --yes

# RUM-based metrics (api/v2/rum/config/metrics)
ddx rum metrics list
ddx rum metrics create --id rum.sessions.web.count --event-type session --compute count --yes
```

### `traces` — APM Traces & Spans

```bash
ddx traces search --query "service:web status:error" --from 1h
ddx traces get TRACE_ID
ddx traces list --service web --from 1h
ddx traces waterfall TRACE_ID                                         # full span tree, indented on TTY
ddx traces waterfall TRACE_ID --json                                  # nested {span, children} tree
ddx traces waterfall TRACE_ID --limit 20                              # cap rendered spans, truncation noted on stderr
```

`waterfall` fetches every span in the trace and reconstructs the parent/child tree client-side (native now — no longer requires the Datadog MCP server).

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

# Per-function delta instead of per-endpoint — joined by (function, file), frame indices aren't stable across calls
ddx profile diff --service web-1000farmacie --type alloc-samples --by function \
  --before-version v2026.4.57 --after-version v2026.4.58 --from 2d --top 30

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

**Known limitations**: the Datadog Profiler API does NOT support endpoint-scoped flame graphs (`--by function` is always cross-endpoint; verified against the UI which has the same limitation). Heap-live samples have no endpoint attribution in Ruby — `--type heap-live-samples --by endpoint` returns 100 % `_UNASSIGNED_` and the CLI emits a stderr hint suggesting `--by function` instead. `diff` pre-flight rejects `--query "@endpoint:..."` (not a real filter tag — silently matches nothing) and warns on stderr when `profiles_in_window` is 0 on either side (comparison may be unrepresentative).

**Operational note**: profiling is currently disabled org-wide, so most windows report `profiles_in_window: 0`; `aggregate`/`get` short-circuit that case and print `note: 0 profiles in window — nothing to aggregate (is continuous profiling enabled?)` on stderr instead of erroring on an empty decode.

### `services` — Service Catalog & Dependencies

```bash
ddx services list
ddx services get web-1000farmacie
ddx services deps web-1000farmacie --direction downstream
ddx services deps web-1000farmacie --direction upstream --mermaid
ddx services team backend
```

### `scorecards`, `investigations`, `network` — Service Health & Network Visibility

```bash
ddx scorecards list --name Observability
ddx scorecards rules --enabled-only --custom-only
ddx investigations list --monitor-id 12345678             # Bits AI investigations (preview/x-unstable API)
ddx network devices
ddx network connections --group-by client_service,server_service --from 4h
ddx network dns --query "client_service:checkout" --from 1h
```

All three were repointed off dead 404 endpoints this release (`api/v2/scorecards*` → `api/v2/scorecard/*`, `api/v2/security_monitoring/investigations` → `api/v2/bits-ai/investigations`, `api/v2/network/flows` → the two real Cloud Network Monitoring aggregate endpoints, replacing the removed `network flows`). `network connections`/`network dns` need **Cloud Network Monitoring enabled on the org** — without it Datadog returns a bare 400 (not 403) even for an otherwise valid request.

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
ddx hosts list --all                    # loop start/count pagination until total_matching is reached (capped 20 pages)
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
ddx downtimes create --scope "env:prod" --monitor-tags "service:checkout" --message "Deploy window" --yes
ddx downtimes create --scope "env:staging" --monitor-id 123 --start now --end 2h --yes
```

`create` schedules a **one-time** downtime only (no recurrence modeling). Omit `--start`/`--end` for a downtime that starts immediately and never ends. Bare durations in `--start`/`--end` are **future-anchored** (`--end 2h` = two hours from now, unlike the lookback semantics of `--from`), and `--end` must be after `--start` (validated before sending).

### `synthetics` — Synthetic Tests

```bash
ddx synthetics list
ddx synthetics get TEST_ID
```

### `on-call` — On-Call Management

```bash
ddx on-call schedules --team backend
ddx on-call responders --schedule SCHEDULE_UUID                       # previous/current/next responders
ddx on-call page --team-handle my-team --title "DB replica lag" --urgency high --yes
```

`ddx on-call teams` is a **deprecated alias** — `api/v2/on-call/teams` no longer exists; it now calls `api/v2/team` under the hood. Use `ddx teams list` directly instead.

### `teams` — Datadog Teams

```bash
ddx teams list
ddx teams list --query platform
ddx teams list --mine
ddx teams get platform-team                                           # by UUID or handle; includes memberships by default
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
ddx cost dimensions                                                   # active billing dimensions, for building --fields
ddx cost attribution --month 2026-06                                  # monthly cost by tag (api/v2/cost_by_tag/monthly_cost_attribution)
ddx cost attribution --month 2026-01 --end-month 2026-06 --tags team,env --sort-name infra_host --sort-direction desc
```

`attribution` replaces the dead v1 `cost_by_org` path; it auto-paginates (capped 10 pages, 5s sleep between pages per Datadog's rate-limit guidance) and becomes available no later than the 19th of the following month. `--queries`/`--formulas` on `cost query` are repeatable, same top-level-aware comma-splitting as `metrics query`.

### `usage` — Datadog Usage Metering

```bash
ddx usage summary --from 30d
ddx usage hourly --families logs --from 24h                          # hourly usage by product family, auto-paginated
ddx usage estimated                                                   # current-month estimated cost (delayed up to 72h)
ddx usage historical --month 2026-05                                  # available no later than the 16th of next month
ddx usage projected                                                   # current month only, available ~12th of the month
ddx usage billable --month 2026-06                                    # billable summary (parent-level orgs only)
ddx usage top-metrics --limit 1000                                    # custom metrics by hourly average
ddx usage logs-by-index --from 7d --index-name main,pci
ddx usage billing-dimensions                                          # dimension → usage-endpoint key mapping
```

`estimated`/`historical`/`projected`/`billable`/`billing-dimensions` all default `--month` to the current UTC month when omitted. `historical` and `billing-dimensions` are parent-level-organization only.

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
