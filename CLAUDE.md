# CLAUDE.md — ddx

Go CLI for Datadog. Single binary, JSON output, API key + App key auth. 34 command groups, SQL log parser, multi-metric formulas, APM stats. Scores 84/90 in empirical comparison — primary Datadog tool alongside MCP (70/90) for JOINs.

**API**: Datadog REST API v1/v2. Base URL derived from `DD_SITE`: `datadoghq.eu` → `https://api.datadoghq.eu`.

## Authentication

Resolution order (first non-empty wins):

1. `--api-key` / `--app-key` / `--site` flags
2. `DD_API_KEY` / `DD_APP_KEY` / `DD_SITE` env vars
3. `~/.config/ddx/config.toml` — project from `--project` flag, then `default_project`

Required App Key scopes: `logs_read_data`, `metrics_read`, `monitors_read`, `dashboards_read`, `incidents_read`, `rum_read`, `hosts_read`, `timeseries_query`.

### Multi-project config

```toml
default_project = "production"

[projects.production]
api_key = "31c3adfe..."
app_key = "dc2c1c7c..."
site = "datadoghq.eu"
```

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-key` | — | DD_API_KEY override |
| `--app-key` | — | DD_APP_KEY override |
| `--site` | `datadoghq.eu` | DD_SITE override |
| `--project` | — | Named project from config |
| `--json` | false | Force JSON output |
| `--jq` | — | gjson path filter |
| `--from` | `1h` | Time range start (1h, 7d, now-2h, RFC3339, epoch) |
| `--to` | `now` | Time range end |
| `--limit` | 50 | Max results |
| `--verbose` | false | Print request/response to stderr |
| `--quiet` | false | Suppress non-error output |

## Commands

### logs

```bash
ddx logs search --query "status:error" --from 1h
ddx logs search --query "service:web kube_namespace:backend-prod" --from 4h --limit 20
ddx logs aggregate --query "status:error" --compute "count" --group-by "service" --from 1h
ddx logs aggregate --query "service:web" --compute "avg(@duration)" --group-by "@http.status_code"
ddx logs sql "SELECT service, COUNT(*) FROM logs WHERE status = 'error' GROUP BY service LIMIT 10" --from 1h
```

**API**: `POST /api/v2/logs/events/search` (search), `POST /api/v2/logs/analytics/aggregate` (aggregate, sql)

**logs search flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `*` | Datadog search query |
| `--sort` | `-timestamp` | Sort: `timestamp` or `-timestamp` |
| `--storage` | — | Storage tier: `indexes`, `flex`, `online-archives` |

**logs aggregate flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `*` | Datadog search query |
| `--compute` | `count` | Aggregation: `count`, `avg(@field)`, `sum(@field)`, `min(@field)`, `max(@field)` |
| `--group-by` | — | Field to group by |

**logs sql**: Parses SQL and translates to aggregate API. See [SQL Parser](#sql-parser) section.

### monitors

```bash
ddx monitors list
ddx monitors list --tags "env:production" --status alert
ddx monitors get 12345678
ddx monitors search --query "status:alert"
ddx monitors search --query "notification:slack-ops AND priority:p2"
ddx monitors mute 12345678
ddx monitors unmute 12345678
```

**API**: `GET /api/v1/monitor` (list), `GET /api/v1/monitor/search` (search)

**monitors list flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tags` | — | Filter by tags |
| `--status` | — | Client-side filter: `alert`, `warn`, `ok`, `no_data` |

**monitors search flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | — | Rich search: title, status, team, priority, tag, notification |
| `--sort` | — | Sort: title, status, id, type, created |

### metrics

```bash
ddx metrics query --queries "avg:system.cpu.user{*}" --from 1h
ddx metrics query --queries "avg:cpu{*}","avg:mem{*}" --formulas "query0 + query1"
ddx metrics query --queries "avg:trace.hits{*}" --formulas 'anomalies(query0, "basic", 2)'
ddx metrics query --queries "avg:cpu{*} by {host}" --formulas 'top(query0, 10, "mean", "desc")'
ddx metrics list --name-filter "system.cpu"
ddx metrics metadata system.cpu.user
ddx metrics context system.cpu.user --include-tags --include-assets
ddx metrics submit --metric custom.gauge --value 42 --tags "env:prod"
```

**API**: `POST /api/v2/query/timeseries` (query), `GET /api/v2/metrics` (list)

**metrics query flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--queries` | — | Metric queries (required, comma-separated for multiple) |
| `--formulas` | — | Formula expressions referencing query0, query1, etc. |
| `--interval` | — | Time bucket interval in milliseconds |
| `--raw` | false | Raw CSV instead of binned stats |
| `--cloud-cost` | false | Query Cloud Cost Management data |

**metrics context flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--include-tags` | false | Include all tag values |
| `--include-assets` | false | Include related dashboards/monitors/SLOs |
| `--scope-tags` | — | Pre-filter tags (comma-separated) |

### incidents

```bash
ddx incidents list
ddx incidents list --query "state:active severity:SEV-1"
ddx incidents list --query "(state:active OR state:stable) AND team:backend"
ddx incidents get INCIDENT_ID --timeline
ddx incidents facets --query "state:active"
```

**API**: `GET /api/v2/incidents/search`

**incidents list flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `state:active` | Faceted search: state, severity, team, commander, etc. |
| `--sort` | `-created` | Sort: created, -created, resolved, -severity, etc. |

**incidents get flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--timeline` | false | Include timeline with comments and status changes |

### error-tracking

```bash
ddx error-tracking issues search --from 1d
ddx error-tracking issues search --query "service:web" --persona backend --from 7d
ddx error-tracking issues get ISSUE_UUID
```

**API**: `POST /api/v2/error-tracking/issues/search`

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `*` | Filter query |
| `--persona` | `backend` | Error source: `backend`, `frontend`, `mobile` |

### dashboards

```bash
ddx dashboards list
ddx dashboards get DASHBOARD_ID
ddx dashboards search --query "backend"
```

**API**: `GET /api/v1/dashboard`

### rum

```bash
ddx rum apps
ddx rum events --query "@type:error" --from 1h
ddx rum events --query "@type:view @view.loading_time:>5000" --from 24h --detailed
ddx rum sessions --from 1h
```

**API**: `GET /api/v2/rum/applications` (apps), `POST /api/v2/rum/events/search` (events)

Event types: `session`, `view`, `action`, `error`, `resource`, `long_task`, `vital`

### traces

```bash
ddx traces search --query "service:web status:error" --from 1h
ddx traces get TRACE_ID
ddx traces list --service web --from 1h
```

**API**: `POST /api/v2/spans/events/search`

### services

```bash
ddx services list
ddx services get web-1000farmacie
ddx services deps web-1000farmacie --direction downstream
ddx services deps web-1000farmacie --direction upstream --mermaid
ddx services team backend
```

**API**: `GET /api/v2/services/definitions`, `GET /api/v1/service_dependencies`

| Flag | Default | Description |
|------|---------|-------------|
| `--direction` | `downstream` | `upstream` or `downstream` |
| `--mermaid` | false | Output as Mermaid diagram |

### notebooks

```bash
ddx notebooks list
ddx notebooks get 12345
ddx notebooks search --query "investigation"
ddx notebooks create --name "Title" --cells '[{"type":"markdown","data":"# Summary"}]' --type investigation
ddx notebooks edit 12345 --cells '[...]' --append
ddx notebooks delete 12345
```

**API**: `GET/POST/PUT/DELETE /api/v1/notebooks`

Cell types: `markdown` (text), `metric` (timeseries graph), `logs` (log stream)

### hosts

```bash
ddx hosts list
ddx hosts list --filter "prod"
```

**API**: `GET /api/v1/hosts`

### slos

```bash
ddx slos list
ddx slos get SLO_ID
ddx slos history SLO_ID --from 7d
```

### downtimes

```bash
ddx downtimes list
ddx downtimes get ID
ddx downtimes cancel ID
```

### synthetics

```bash
ddx synthetics list
ddx synthetics get TEST_ID
```

### on-call

```bash
ddx on-call teams
ddx on-call schedules --team backend
```

### events

```bash
ddx events search --query "source:deploy" --from 24h
```

### audit-logs

```bash
ddx audit-logs search --from 24h
ddx audit-logs search --query "@action:modified" --from 7d
```

### security

```bash
ddx security rules
ddx security signals --query "status:high" --from 24h
```

### users, tags, cicd, cases, cloud, cost, usage

```bash
ddx users list
ddx tags list
ddx cicd pipelines
ddx cases list
ddx cloud aws
ddx cost query --queries "sum:all.cost{*}.rollup(sum,daily)" --from 7d
ddx usage summary --from 30d
```

### service-catalog, scorecards, investigations, network

```bash
ddx service-catalog list
ddx scorecards list
ddx investigations list
ddx network devices
```

### overview

```bash
ddx overview --from 1h
```

Parallel fetch: monitors in alert, active incidents, top error tracking issues, error log count.

### config

```bash
ddx config add production --api-key KEY --app-key KEY --site datadoghq.eu
ddx config remove production
ddx config list
ddx config use production
ddx config current
```

## SQL Parser

`ddx logs sql` parses SQL and translates to the Datadog aggregate API (`POST /api/v2/logs/analytics/aggregate`).

### Supported SQL

| SQL | Translates To |
|-----|---------------|
| `COUNT(*)` | `compute: [{aggregation: "count"}]` |
| `AVG(@duration)` | `compute: [{aggregation: "avg", metric: "@duration"}]` |
| `SUM(@field)`, `MIN`, `MAX` | Same pattern |
| `WHERE status = 'error'` | `filter.query: "status:error"` |
| `WHERE a = 'x' AND b = 'y'` | `filter.query: "a:x b:y"` |
| `WHERE @code > 400` | `filter.query: "@code:>400"` |
| `WHERE service IN ('a','b')` | `filter.query: "service:(a OR b)"` |
| `GROUP BY field` | `group_by: [{facet: "field"}]` |
| `ORDER BY ... DESC` | `group_by.sort.order: "desc"` |
| `LIMIT N` | `group_by.limit: N` |

### Not Supported

JOIN, HAVING, subqueries, CTEs, DATE_TRUNC, window functions, multiple aggregates per query. Shows error pointing to Datadog MCP `analyze_datadog_logs` for complex SQL.

## Output

- **TTY**: Tables (go-pretty) for commands with table definitions, JSON otherwise
- **Piped**: Always JSON
- `--json`: Force JSON on TTY
- `--jq`: gjson filter (NOT jq syntax). Array: `#.field`. Object: `#.{a:a,b:b}`

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | API/network error |
| 2 | Auth error (401/403) |
| 4 | Not found (404) |

## Architecture

```
cmd/ddx/main.go              Entry point, version injection, exit codes
internal/
  client/client.go            HTTP client, DD auth headers, retries, dual pagination
  client/errors.go            APIError with ExitCode(), hints
  commands/root.go            Root command, global flags, getClient()
  commands/*.go               One file per command group (33 files)
  config/config.go            TOML config, multi-project, credential resolution
  output/output.go            JSON/table dispatcher, TTY detection
  output/table.go             go-pretty table rendering, column definitions
  output/filter.go            gjson --jq filter
  sqlparse/sqlparse.go        SQL tokenizer + parser for logs sql
  sqlparse/sqlparse_test.go   Parser tests (16 cases)
  timeparse/timeparse.go      Relative/absolute/epoch time parsing
```

## HTTP Client

- **Auth**: `DD-API-KEY` + `DD-APPLICATION-KEY` headers
- **Base URL**: `https://api.{DD_SITE}` (e.g., `https://api.datadoghq.eu`)
- **Timeout**: 30s per request
- **Retries**: Max 3 on 429 or 5xx, exponential backoff (1s, 2s, 4s) + jitter
- **Pagination**: Cursor-based (v2 APIs: logs, RUM, spans) and offset-based (v1 APIs: monitors, dashboards)
- **Error parsing**: Handles both v1 (`{"errors": ["msg"]}`) and v2 (`{"errors": [{"detail": "msg"}]}`) formats
