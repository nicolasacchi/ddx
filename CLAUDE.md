# CLAUDE.md — ddx

Go CLI for Datadog. Single binary, JSON output, API key + App key auth. 34 command groups, SQL log parser, multi-metric formulas + scalar queries, custom-metric cardinality cost control, APM stats, **continuous-profiler aggregation** (flame graph + per-endpoint hotspots + per-version diff), full trace waterfalls, Usage Metering v2 + cost attribution billing visibility. Scores 84/90 in empirical comparison — primary Datadog tool alongside MCP (70/90) for log SQL JOINs.

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
| `--yes` / `--confirm` | false | Confirm destructive operations (mute/delete/cancel/resolve/submit) |
| `--dry-run` | false | Print the intended mutation and exit without sending |

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

**Log cost-control config groups** (`logs metrics`, `logs archives`, `logs destinations` — each `list`/`get`/`create`/`delete`, `archives`/`destinations` also `update`):

```bash
ddx logs metrics create --name logs.bot.count --query "@user_agent.type:crawler" --compute count --yes
ddx logs metrics create --name logs.page.load.count --query "service:web*" --compute "distribution/@duration" --group-by "@http.status_code:status_code" --include-percentiles --yes
ddx logs archives create --name "Nginx Archive" --query "source:nginx" \
  --destination-json '{"type":"s3","bucket":"my-bucket","integration":{"account_id":"123456789012","role_name":"my-role"}}' --yes
ddx logs destinations create --name "Nginx logs" --query "source:nginx" \
  --forwarder-json '{"type":"http","endpoint":"https://example.com","auth":{"type":"basic","username":"u","password":"p"}}' --yes
```

**API**: `GET/POST/DELETE /api/v2/logs/config/metrics` (metrics), `GET/POST/PUT/DELETE /api/v2/logs/config/archives` (archives), `GET/POST/PUT/DELETE /api/v2/logs/config/custom-destinations` (destinations)

| Command | Flag | Default | Description |
|---------|------|---------|-------------|
| `logs metrics create` | `--name` | — | Log-based metric name, e.g. `logs.page.load.count` (required) |
| | `--query` | — | Log search query to filter on |
| | `--compute` | — | `"count"` or `"<aggregation_type>/<path>"`, e.g. `"distribution/@duration"` (required) |
| | `--group-by` | — | Group-by rule(s) `"path[:tag_name]"`, repeatable |
| | `--include-percentiles` | false | Percentile aggregations (distribution metrics only) |
| `logs archives create` | `--name` | — | Archive name (required) |
| | `--query` | — | Filter — matching logs are included (required) |
| | `--destination-json` | — | Raw JSON for the S3/GCS/Azure destination union — not modeled flag-by-flag (required) |
| | `--compression-method` | — | `GZIP` or `ZSTD` |
| | `--include-tags` | false | Store tags in the archive (default: stripped) |
| | `--rehydration-max-scan-gb` | — | Max scan size in GB for rehydration |
| | `--rehydration-tags` | — | Tags added to rehydrated logs, e.g. `team:intake` |
| `logs destinations create` | `--name` | — | Custom destination name (required) |
| | `--forwarder-json` | — | Raw JSON for the http/splunk/elasticsearch/microsoft_sentinel union — not modeled flag-by-flag (required) |
| | `--enabled` | true | Whether matching logs are forwarded |
| | `--forward-tags` | true | Whether tags from forwarded logs are forwarded |
| | `--forward-tags-restriction-list` / `-list-type` | — | Tag keys to filter + `ALLOW_LIST`/`BLOCK_LIST` |

All four `create`/`delete`(/`update`) verbs are write-gated — see the **Write-safety gate** note under `incidents` below.

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
ddx metrics query --metric system.cpu.user --from 1h                           # shortcut: builds avg:<metric>{*}
ddx metrics query --queries "avg:system.cpu.user{*}" --interval 1h30m --from 24h  # Go duration or 1d/2w suffix, not just millis
ddx metrics query --queries "sum:system.disk.used{*}" --strict-types --from 1h   # error (not warn) on sum: over a gauge
ddx metrics list --name-filter "system.cpu"
ddx metrics metadata system.cpu.user
ddx metrics context system.cpu.user --include-tags --include-assets
ddx metrics submit --metric custom.gauge --value 42 --tags "env:prod"
```

**API**: `POST /api/v2/query/timeseries` (query), `GET /api/v2/metrics` (list)

**metrics query flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--queries` | — | Metric queries. Repeatable (`--queries "avg:a{*}" --queries "avg:b{*}"`); a single value may also comma-join multiple queries — the split happens outside `{}`/`()`/quotes via the shared `splitQueriesTopLevel` helper (`internal/commands/querysplit.go`), so `"avg:a{x:1,y:2},avg:b{*}"` still works as one value |
| `--metric` | — | Shortcut for a single query: builds `avg:<metric>{*}`. Mutually exclusive with `--queries` |
| `--formulas` | — | Formula expressions referencing query0, query1, etc. Repeatable, same comma-splitting as `--queries` |
| `--interval` | — | Time bucket interval: milliseconds (back-compat, e.g. `300000`), a Go duration (`90s`, `1h30m`), or a day/week suffix (`1d`, `2w`) |
| `--raw` | false | Raw CSV instead of binned stats |
| `--strict-types` | false | Error instead of warn when a `sum:` query targets a gauge metric (see below) |
| `--cloud-cost` | false | Query Cloud Cost Management data |

`sum:` applied to a gauge performs a sum temporal rollup and inflates results — a mistake worth flagging. By default `metrics query` prints `warning: <query>: sum: on a gauge applies a sum temporal rollup and inflates results - use avg:/max:, or as_count() on counters` to stderr and proceeds; `--strict-types` turns that into a hard error instead.

**metrics scalar** — one aggregated number per group (Query Value/Table/Toplist widgets), as opposed to `query`'s timeseries:

```bash
ddx metrics scalar --queries "avg:system.cpu.user{*} by {env}" --from 1h
ddx metrics scalar --queries "sum:trace.web.request.hits{*}","sum:trace.web.request.errors{*}" \
  --formulas "query1 / query0" --reducer sum --from 4h
```

| Flag | Default | Description |
|------|---------|-------------|
| `--queries` | — | Metric queries, e.g. `"avg:system.cpu.user{*} by {env}"` |
| `--formulas` | — | Formula expressions (e.g. `"query1 / query0"`) |
| `--reducer` | `avg` | Aggregator applied to each query: `avg`\|`last`\|`max`\|`min`\|`sum` |

**metrics context flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--include-tags` | false | Include all tag values |
| `--include-assets` | false | Include related dashboards/monitors/SLOs |
| `--scope-tags` | — | Pre-filter tags (comma-separated) |

**Custom-metric cardinality cost control** (`internal/commands/metrics_cardinality.go`) — the levers for finding and capping expensive custom metrics before they blow up the bill:

```bash
ddx metrics volumes system.cpu.user --window 7200
ddx metrics estimate dist.request.latency --groups app,host --pct --hours-ago 49
ddx metrics tag-cardinalities system.cpu.user
ddx metrics tags get http.endpoint.request
ddx metrics tags set http.endpoint.request --metric-type distribution --tags app,datacenter --yes
ddx metrics tags set http.endpoint.request --metric-type distribution --tags app --exclude-tags-mode --yes
ddx metrics tags delete http.endpoint.request --yes
ddx metrics tag-rules list --search my-rule
ddx metrics tag-rules create --name my-rule --metric-name-matches "dd.test.*" --tags env,service --yes
ddx metrics tag-rules update <uuid> --tags env,service,region --yes
ddx metrics tag-rules reorder <uuid-2> <uuid-1> --yes
ddx metrics tag-rules delete <uuid> --yes
```

**API**: `GET /api/v1/metrics/{name}` volumes+estimate (Metrics without Limits™), `GET /api/v2/metrics/{name}/tags` cardinality, `GET/POST/PATCH/DELETE /api/v2/metrics/{name}/tags` tag config, `GET/POST/PATCH/DELETE /api/v2/metrics/tag-configuration` tag-rules (successor to deprecated `config/bulk-tags`, preview/unstable per spec)

| Command | Flag | Default | Description |
|---------|------|---------|-------------|
| `volumes <metric>` | `--window` | 3600 | Look-back window in seconds, max 2592000 |
| `estimate <metric>` | `--groups` | — | Comma-separated tag keys the metric is queried with, e.g. `app,host` |
| | `--pct` | false | Include percentile aggregators (distribution metrics only) |
| | `--hours-ago` / `--timespan-h` | — / 1 | Look-back offset / window in hours |
| `tags set <metric>` | `--metric-type` | — | `gauge`\|`count`\|`rate`\|`distribution` (required; can't change once a configuration exists) |
| | `--tags` | — | Comma-separated tag keys to make queryable (required) |
| | `--exclude-tags-mode` | false | Treat `--tags` as a deny-list instead of allow-list |
| | `--include-percentiles` | false | Distribution metrics only |
| `tag-rules create` | `--name` | — | Human-readable rule name (required) |
| | `--metric-name-matches` | — | Comma-separated glob patterns the rule applies to (required) |
| | `--tags` | — | Tag keys managed by the rule |
| | `--exclude-tags-mode` | false | Deny-list instead of allow-list |
| | `--ignored-metric-name-matches` | — | Metric name prefixes excluded from scope |
| `tag-rules update <id>` | (same flags, all optional) | — | Omitted flags leave the attribute unchanged; `--rule-order` conflicts return 409 — use `reorder` for atomic re-sequencing |
| `tag-rules reorder <id...>` | — | — | Assigns `rule_order` 1,2,... matching each UUID's position in the arg list |

`tags set` is create-or-update: tries create first, falls back to a partial update on 409. `tag-rules create` assigns `rule_order` server-side as max+1; `tag-rules delete` is idempotent and re-sequences remaining rules to stay dense/1-based. All of `tags set`/`delete` and `tag-rules create`/`update`/`delete`/`reorder` are write-gated.

### incidents

```bash
ddx incidents list
ddx incidents list --all
ddx incidents list --query "state:active severity:SEV-1"
ddx incidents list --query "(state:active OR state:stable) AND team:backend"
ddx incidents get INCIDENT_ID --timeline
ddx incidents facets --query "state:active"
ddx incidents create --title "Checkout errors spiking" --severity SEV-2 --yes
ddx incidents create --title "Elevated 500s" --customer-impacted --customer-impact-scope "EU checkout" --commander <user-uuid> --yes
ddx incidents update 45 --severity SEV-3 --summary "Re-classified after triage"
ddx incidents update 45 --root-cause-file ./rca.md
ddx incidents resolve 45 --root-cause "Backfill drained; self-healed"
```

**API**: `GET /api/v2/incidents/search` (list/facets), `POST /api/v2/incidents` (create), `GET /api/v2/incidents/{uuid}` (get + public-id→UUID resolution), `PATCH /api/v2/incidents/{uuid}` (update/resolve)

**incidents list flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `state:active` | Faceted search: state, severity, team, commander, etc. |
| `--sort` | `-created` | Sort: created, -created, resolved, -severity, etc. |
| `--all` | false | Fetch every page (loops `page[offset]`/`page[size]=100`, capped at 20 pages) instead of just the first. Without it, a truncated first page prints `incidents.list: showing X of Y (use --all to fetch every page)` to stderr |

**incidents create flags:**

| Flag | Description |
|------|-------------|
| `--title` | Incident title (required) |
| `--severity` | `SEV-1` … `SEV-5` |
| `--summary` | Incident summary (mapped into `fields`, same shape as `update`) |
| `--customer-impacted` | Flag the incident as customer-impacting |
| `--customer-impact-scope` | Required when `--customer-impacted` is set |
| `--commander` | Datadog user UUID; sent as `relationships.commander_user`, not a `fields` entry |

`create` **never sets `fields.slug` or `public_id`** — Datadog assigns the incident number on creation (printed to stderr as `Created incident <public_id>: "<title>" (uuid=...)` on success), matching repo policy that `IR-` numbers belong to Datadog and are never invented locally.

**incidents get flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--timeline` | false | Include timeline with comments and status changes |

**incidents update flags** (PATCH — only flags you pass are sent; other fields are left untouched):

| Flag | Description |
|------|-------------|
| `--state` | `active`, `stable`, `resolved` (mapped to `fields.state.value`) |
| `--severity` | `SEV-1` … `SEV-5` (mapped to `fields.severity.value`) |
| `--root-cause` | Inline text for `fields.root_cause.value` |
| `--root-cause-file` | Path to file (or `-` for stdin) holding the root-cause text. Mutually exclusive with `--root-cause` |
| `--summary` | Inline text for `fields.summary.value` |
| `--resolved` | RFC3339 timestamp or `now`; auto-set to `now` when `--state=resolved` and not supplied explicitly |

**incidents resolve flags** (shortcut for `update --state resolved --resolved now`):

| Flag | Description |
|------|-------------|
| `--root-cause` | Inline text for `fields.root_cause.value` |
| `--root-cause-file` | Path/`-` for stdin. Mutually exclusive with `--root-cause` |

**ID resolution**: `update` and `resolve` accept the public id (`45`) or the UUID. Public ids trigger one `GET /api/v2/incidents/{id}` to fetch the UUID before the PATCH (the PATCH endpoint requires UUID, not public id). Pass the UUID directly to skip the round-trip.

**Field shape**: each field is sent as `{"type": "...", "value": "..."}`. ddx hard-codes the type for the documented fields (`state`/`severity` → `dropdown`, `root_cause`/`summary` → `textbox`) via `incidentFieldTypes` in `internal/commands/incidents.go`. Add an entry there to surface a new field.

**Write-safety gate**: state-changing verbs refuse unless `--yes`/`--confirm` is passed, exiting code `6` (`write_locked`); `--dry-run` previews the change and sends nothing. Gated verbs (all route through the same `requireConfirm()` in `internal/commands/confirm.go`):

- `incidents create`/`update`/`resolve`
- `monitors mute`/`delete`
- `downtimes create`/`cancel`
- `notebooks delete`
- `metrics submit`
- `metrics tags set`/`delete`
- `metrics tag-rules create`/`update`/`delete`/`reorder`
- `rum retention-filters create`/`update`/`delete`
- `rum metrics create`/`delete`
- `logs metrics create`/`delete`
- `logs archives create`/`update`/`delete`
- `logs destinations create`/`update`/`delete`
- `error-tracking issues set-state`/`assign`
- `on-call page`

Reads and recovery ops (`monitors unmute`, `notebooks create`/`edit`) are ungated. Automation must pass `--yes` explicitly. Same `DD_APP_KEY` is used for reads and writes.

### error-tracking

```bash
ddx error-tracking issues search --from 1d
ddx error-tracking issues search --query "service:web" --persona backend --from 7d
ddx error-tracking issues search --persona all --from 1d       # fans out backend+frontend+mobile, merges results
ddx error-tracking issues get ISSUE_UUID
ddx error-tracking issues set-state ISSUE_UUID --state RESOLVED --yes
ddx error-tracking issues assign ISSUE_UUID --assignee user@example.com --yes
```

**API**: `POST /api/v2/error-tracking/issues/search` (search), `GET .../issues/{id}` (get), `PUT .../issues/{id}/state` (set-state), `PUT .../issues/{id}/assignee` (assign)

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `*` | Filter query |
| `--persona` | `backend` | Error source: `backend`, `frontend`, `mobile`, or `all` (fans out all three and merges) |

**issues set-state flags:**

| Flag | Description |
|------|-------------|
| `--state` | `OPEN`, `ACKNOWLEDGED`, `RESOLVED`, `IGNORED`, `EXCLUDED` (required) |

**issues assign flags:**

| Flag | Description |
|------|-------------|
| `--assignee` | Datadog user UUID, or an email resolved via `GET /api/v2/users` (required) |

`set-state` and `assign` are write-gated. `search` truncates results to `--limit` client-side after merging (relevant with `--persona all`, since the merge can exceed the limit) and prints `error-tracking.search: showing X of Y (raise --limit to see more)` to stderr when truncated.

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
ddx rum aggregate --query "@type:error" --compute "count" --group-by "@view.url" --from 1h
ddx rum aggregate --query "@type:view" --compute "avg(@view.loading_time)" --group-by "@session.type" --from 4h
```

**API**: `GET /api/v2/rum/applications` (apps), `POST /api/v2/rum/events/search` (events), `POST /api/v2/rum/analytics/aggregate` (aggregate)

Event types: `session`, `view`, `action`, `error`, `resource`, `long_task`, `vital`

**rum aggregate flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--query` | `*` | RUM search query |
| `--compute` | `count` | `count`, `avg(@field)`, `sum(@field)`, `min(@field)`, `max(@field)`, `cardinality(@field)` |
| `--group-by` | — | Facet to group by (e.g., `@view.url`, `@session.type`) |

**rum retention-filters** — which RUM events a `--app` actually retains, `list`/`get`/`create`/`update`/`delete` (scoped by `--app`, required on every subcommand):

```bash
ddx rum retention-filters list --app APP_ID
ddx rum retention-filters create --app APP_ID --name "Errors only" --event-type error --sample-rate 100 --yes
ddx rum retention-filters update FILTER_ID --app APP_ID --sample-rate 50 --yes
ddx rum retention-filters delete FILTER_ID --app APP_ID --yes
```

**API**: `GET/POST/PUT/DELETE /api/v2/rum/applications/{app_id}/retention_filters`

| Flag | Default | Description |
|------|---------|-------------|
| `--app` | — | RUM application ID (required, global to the subcommand group) |
| `--name` | — | Retention filter name (required on create) |
| `--event-type` | — | `session`, `view`, `action`, `error`, `resource`, `long_task`, `vital` (required on create) |
| `--sample-rate` | — | Between 0.1 and 100 (required on create) |
| `--query` | — | RUM search query to filter on |
| `--enabled` | true | Whether the filter is enabled |

**rum metrics** — RUM-based metrics, `list`/`get`/`create`/`delete`:

```bash
ddx rum metrics list
ddx rum metrics create --id rum.sessions.web.count --event-type session --compute count --yes
ddx rum metrics create --id rum.view.duration --event-type view --compute "distribution/@duration" \
  --include-percentiles --group-by "@browser.name:browser_name" --yes
ddx rum metrics delete rum.sessions.web.count --yes
```

**API**: `GET/POST/DELETE /api/v2/rum/config/metrics`

| Flag | Default | Description |
|------|---------|-------------|
| `--id` | — | RUM-based metric name, e.g. `rum.sessions.web.count` (required) |
| `--event-type` | — | Same enum as retention-filters (required) |
| `--compute` | — | `"count"` or `"<aggregation_type>/<path>"`, e.g. `"distribution/@duration"` (required) |
| `--group-by` | — | Group-by rule(s) `"path[:tag_name]"`, repeatable |
| `--include-percentiles` | false | Distribution metrics only |
| `--query` | — | RUM search query filter |
| `--uniqueness-when` | — | `match` or `end` — when to count updatable events (session/view only) |

`retention-filters create`/`update`/`delete` and `metrics create`/`delete` are write-gated.

### traces

```bash
ddx traces search --query "service:web status:error" --from 1h
ddx traces get TRACE_ID
ddx traces list --service web --from 1h
ddx traces waterfall TRACE_ID
ddx traces waterfall TRACE_ID --json
ddx traces waterfall TRACE_ID --limit 20
```

**API**: `POST /api/v2/spans/events/search` (search/list), `GetTraceByID` (waterfall — fetches every span in the trace, no separate list call)

**waterfall**: reconstructs the parent/child span tree client-side. Default TTY output is indented text — one line per span with service, resource (operation name), and duration in ms, marked `[ERROR]` on error spans. `--json` (or piping) instead returns a nested tree: `{"span": {...}, "children": [...]}`. Orphan spans (parent not present in the trace) and true trace-roots both attach under a synthetic root, so nothing is dropped even on a partial/re-parented response. `--limit` caps rendered spans (pre-order, root's children first); truncation is always noted on stderr, never silently dropped — Datadog itself can truncate the raw trace payload server-side (`is_truncated`), which is reported the same way. This command is now **native** — it no longer requires the Datadog MCP server for full trace waterfall views.

### profile

```bash
ddx profile list      --service web-1000farmacie --query "kube_deployment:web-canary" --from 1h --limit 20
ddx profile aggregate --service web-1000farmacie --query "kube_deployment:web-canary" \
                      --type alloc-samples --by endpoint --top 20 --from 7d
ddx profile aggregate --service web-1000farmacie --type cpu-time --by function --top 30 --from 1h
ddx profile summary   --service web-1000farmacie --from 1h
ddx profile diff      --service web-1000farmacie --type alloc-samples \
                      --before-version v2026.4.57 --after-version v2026.4.58 --from 2d --top 20
ddx profile diff      --service web-1000farmacie --type alloc-samples --by function \
                      --before-version v2026.4.57 --after-version v2026.4.58 --from 2d --top 30
ddx profile get       --event-id "<id from list>" --profile-id "<profile-id from list>" --by info
```

**API**: `POST /profiling/api/v1/aggregate` (aggregate, summary, diff, get's flame views), `POST /api/unstable/profiles/list` (list), `GET /profiling/api/v1/profiles/{profileId}/info?eventId=X&eventScope=profile` (get --by info)

The aggregate endpoint is what the Datadog UI calls to render the flame graph. Returns server-aggregated JSON (no raw pprof bytes). Both endpoints accept the standard `DD-API-KEY` + `DD-APPLICATION-KEY` auth and work on `api.datadoghq.eu` and `app.datadoghq.eu`.

**profile shared flags** (list, aggregate, summary, diff):

| Flag | Default | Description |
|------|---------|-------------|
| `--service` | — | Datadog service name (required) |
| `--env` | `production` | Environment |
| `--query` | — | Additional filter, e.g. `"kube_deployment:web-canary"` |

**profile aggregate flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | `cpu-time` | `cpu-time`, `wall-time`, `alloc-samples`, `heap-live-samples`, `heap-live-size` (Ruby — `alloc-bytes` returns 400) |
| `--by` | `endpoint` | `endpoint` (top-N endpoints from `endpointValues`), `function` (top-N flame leaves), `summary` (totals only) |
| `--top` | `20` | Top N results to display |

**profile get flags** (single-profile drill-down — pulls one profile by ID):

| Flag | Default | Description |
|------|---------|-------------|
| `--event-id` | — | Long base64 event id from `ddx profile list` field `id` (required) |
| `--profile-id` | — | Short base64 profile id from `ddx profile list` field `profile-id` (required) |
| `--by` | `info` | View: `info` (rich per-profile metadata + Ruby GC stats), `endpoint`, `function`, `summary` |
| `--type` | `cpu-time` | Profile type for endpoint/function/summary views |
| `--top` | `20` | Top N for endpoint/function views |

`--by info` returns the per-profile metadata: profileStart/End, host, all 60+ tags, system info, **Ruby GC stats** (`heap_live_slots`, `heap_marked_slots`, `minor_gc_count`, `major_gc_count`, `total_allocated_objects`, malloc/oldmalloc increase counters), allocation sampling stats, and full profiler settings. Closest thing to the `runtime.ruby.*` metrics that aren't currently shipping.

`--by endpoint|function|summary` returns the flame-graph data for that one 60-second profile (equivalent to clicking on a profile in the UI explorer's stream view). Internally uses paired `profileIds` + `eventIds` + `eventScopes:["profile"]` arrays in the aggregate request body.

Pipe pattern: `ddx profile list ... --jq '0.{event:id,profile:"profile-id"}'` → copy IDs → `ddx profile get --event-id E --profile-id P`.

**profile diff flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | `cpu-time` | Profile type to compare |
| `--by` | `endpoint` | `endpoint` (per-endpoint delta, joined by endpoint name) or `function` (per-function delta, joined by (function, file) identity — frame indices aren't stable across two independently-captured aggregate responses, so function+file is the only safe join key) |
| `--before-version` | — | Image version tag for the 'before' side, e.g. `v2026.4.57` (one of `--before-version` / `--before-query` required) |
| `--after-version` | — | Image version tag for the 'after' side (one of `--after-version` / `--after-query` required) |
| `--before-query` | — | Arbitrary Datadog filter for the 'before' side (alternative to `--before-version`), e.g. `"pod_name:web-canary-X"` or `"@timestamp:[now-2h TO now-1h]"` |
| `--after-query` | — | Arbitrary Datadog filter for the 'after' side |
| `--top` | `20` | Top N rows by absolute delta |

Use `--before-version` / `--after-version` for the common "did this PR regress?" case. Use `--before-query` / `--after-query` for arbitrary diffs (canary-vs-prod, pod-A-vs-pod-B, peak-hours-vs-off-hours). `--by function` is the "which functions got slower/heavier" cut of the same comparison.

**Pre-flight validations** (`profile diff`, and by extension `aggregate`/`get`): a `--query`/`--before-query`/`--after-query` containing `@endpoint:` is rejected outright — it isn't a real filter tag on profiles and would silently match nothing (`profiles_in_window: 0`) rather than erroring; use `--by endpoint` on the output instead. When `profiles_in_window` is 0 on either side of a diff, ddx still returns the comparison but prints `comparison may be unrepresentative — profiles_in_window is 0 on at least one side (before=X, after=Y)` to stderr.

**Inherited `--limit`** controls how many profile uploads the API aggregates server-side (default 50). Higher = more representative aggregation, slower response. Independent from `--top` (which trims the displayed result list).

**Known API limitations** (verified against Datadog's `/profiling/api/v1/aggregate`):

- **No endpoint-scoped flame graph.** The API has no way to scope `--by function` results to a single endpoint. Per-frame data carries no endpoint dimension; only `endpointValues` (flat per-endpoint totals) is exposed. The Datadog UI's Endpoint facet is display-only and doesn't trigger re-aggregation either. Workaround: use `--by endpoint` to find heavy endpoints + `--by function` to find hot leaves across the deployment, then mentally cross-reference (or enable APM-profile correlation in `dd-trace-rb` so per-endpoint allocation tags land on spans).
- **`--type alloc-bytes` returns HTTP 400 for Ruby.** Caught pre-flight with a Ruby-specific error message. Use `alloc-samples` (allocation count) instead.
- **Heap-live samples have no endpoint attribution.** Ruby's retained-heap profiler emits all samples as `_UNASSIGNED_`. `--type heap-live-samples --by endpoint` will emit a stderr hint suggesting `--by function`. The function view IS informative for retained-heap analysis.
- **Profiling is currently disabled org-wide.** Most windows report `profiles_in_window: 0`. `aggregate`/`get` short-circuit that case (empty decode would otherwise be a confusing error) and print `note: 0 profiles in window — nothing to aggregate (is continuous profiling enabled?)` to stderr instead.

**`--jq` is gjson, not jq** (general ddx note worth restating for profile output):

- `list` returns a flat array. Use `0` for first element, `#.id` for all IDs — NOT `data.0` (no `data` key after flattening).
- `aggregate --by endpoint` returns `{top: [...], total, ...}`. Use `top.0.endpoint` for top endpoint, `top.#.endpoint` for the list, `top.#.{e:endpoint,v:value}` for projection.
- `aggregate --by function` returns `{top: [...], ...}`. Use `top.#.function`, `top.#.file`, etc.
- `get --by info` returns the per-profile info object directly. Use `customData.internal.gc.heap_live_slots`, `customData.internal.gc.minor_gc_count`, `tags.#`, `systemInfo.runtime.version`, etc.

**Output shapes:**
- `aggregate --by=endpoint`: `{profile_type, profiles_aggregated, profiles_in_window, total, top: [{endpoint, value, percent_of_total}, ...], metadata}`
- `aggregate --by=function`: `{profile_type, unique_leaf_frames, total, top: [{function, file, line, library, kind, value, percent_of_total}, ...], metadata}`
- `aggregate --by=summary` / `summary`: `{profiles_aggregated, profiles_in_window, summary_values: {cpu-time, wall-time, alloc-samples, heap-live-samples, heap-live-size, ...}, summary_durations, profile_ids, metadata}`
- `diff`: `{profile_type, before_version, after_version, before_endpoints, after_endpoints, top_by_abs_delta: [{endpoint, before, after, delta, percent_change}, ...], before_metadata, after_metadata}`
- `list`: array of profile attribute objects (id, host, pod_name, version, profiler_version, duration, ingest_size_in_bytes, …)

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
ddx hosts list --all
```

**API**: `GET /api/v1/hosts`

| Flag | Default | Description |
|------|---------|-------------|
| `--filter` | — | Filter hosts by name (substring match) |
| `--all` | false | Fetch every page (loops `start`/`count` pagination until `total_matching` is reached, capped at 20 pages) |

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
ddx downtimes create --scope "env:prod" --monitor-tags "service:checkout" --message "Deploy window" --yes
ddx downtimes create --scope "env:staging" --monitor-id 123 --start now --end 2h --yes
```

**API**: `GET /api/v1/downtime` (list/get), `DELETE /api/v1/downtime/{id}` (cancel), `POST /api/v2/downtime` (create)

**downtimes create flags:**

| Flag | Description |
|------|-------------|
| `--scope` | Scope the downtime applies to, e.g. `"env:(staging OR prod) AND datacenter:us-east-1"` (required) |
| `--monitor-id` | Mute only this monitor id (mutually exclusive with `--monitor-tags`) |
| `--monitor-tags` | Comma-separated monitor tags to mute (mutually exclusive with `--monitor-id`); omit both to mute all monitors in scope |
| `--start` | `now`, RFC3339, epoch, or a bare duration (**future-anchored**: `1h` = one hour from now); omitted = starts immediately |
| `--end` | Same forms (`2h` = two hours from now); must be after `--start` (validated client-side); omitted = never ends |
| `--message` | Message included with downtime notifications |

Only **one-time** schedules are supported — no recurrence modeling. Write-gated.

### synthetics

```bash
ddx synthetics list
ddx synthetics get TEST_ID
```

### on-call

```bash
ddx on-call schedules --team backend
ddx on-call responders --schedule 3653d3c6-0c75-11ea-ad28-fb5701eabc7d
ddx on-call responders --schedule <id> --position previous,current,next
ddx on-call responders --schedule <id> --at 2026-07-20T10:00:00Z
ddx on-call page --team-handle my-team --title "DB replica lag" --urgency high --yes
ddx on-call page --user-id <uuid> --title "Manual escalation" --urgency low --dry-run
```

**API**: `GET /api/v2/on-call/schedules` (schedules), `GET /api/v2/on-call/schedules/{schedule_id}/responders` (responders), `POST /api/v2/on-call/pages` (page)

**on-call responders flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--schedule` | — | Schedule UUID (required) |
| `--position` | `current` | Comma-separated: `previous`,`current`,`next` |
| `--at` | now | RFC3339 timestamp to query responders at |
| `--include` | `responders,responders.shifts,responders.shifts.user` | Comma-separated included relationships |

**on-call page flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--team-id` / `--team-handle` / `--user-id` | — | Target (exactly one required) |
| `--title` | — | Page title (required) |
| `--urgency` | `high` | `low` or `high` |
| `--description` | — | Short summary of the issue or context |
| `--tags` | — | Comma-separated tags, e.g. `service:checkout` |

`page` is write-gated. `ddx on-call teams` is a **deprecated alias** for `ddx teams list` — `api/v2/on-call/teams` no longer exists; the command now calls `api/v2/team` under the hood and prints a deprecation notice.

### teams

Canonical Datadog Teams surface (`api/v2/on-call/teams` doesn't exist; `on-call teams` is kept only as a deprecated alias pointing here).

```bash
ddx teams list
ddx teams list --query platform
ddx teams list --mine --limit 20
ddx teams get 00000000-0000-0000-0000-000000000001
ddx teams get platform-team
ddx teams get platform-team --memberships=false
```

**API**: `GET /api/v2/team` (list), `GET /api/v2/team/{team_id}` (get), `GET /api/v2/team/{team_id}/memberships` (get, merged in by default)

| Command | Flag | Default | Description |
|---------|------|---------|-------------|
| `list` | `--query` | — | Search by team name, handle, or member email (`filter[keyword]`) |
| | `--mine` | false | Only teams the current user belongs to (`filter[me]`) |
| `get <id-or-handle>` | `--memberships` | true | Also fetch and merge team memberships into the output |

`get` only supports lookup by UUID at the API level; if the argument doesn't look like a UUID, ddx lists teams with `filter[keyword]=<arg>` and matches by exact handle before falling back to the by-ID call.

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

### users, tags, cicd, cases, cloud

```bash
ddx users list
ddx tags list
ddx cicd pipelines
ddx cases list
ddx cloud aws
```

### cost

```bash
ddx cost query --queries "sum:all.cost{*}.rollup(sum, daily)" --from 7d
ddx cost query --queries "sum:aws.cost.amortized{service:ec2}" --from 30d
ddx cost dimensions
ddx cost attribution --month 2026-06
ddx cost attribution --month 2026-01 --end-month 2026-06 --fields "infra_host_on_demand_cost,infra_host_percentage_in_account"
ddx cost attribution --month 2026-06 --tags team,env --sort-name infra_host --sort-direction desc
```

**API**: `POST /api/v2/query/timeseries` (query, cloud-cost path), `GET /api/v2/cost_by_tag/active_billing_dimensions` (dimensions), `GET /api/v2/cost_by_tag/monthly_cost_attribution` (attribution — replaces the dead v1 `cost_by_org`)

| Command | Flag | Default | Description |
|---------|------|---------|-------------|
| `query` | `--queries` | — | Repeatable, same top-level-aware comma-splitting as `metrics query` |
| | `--formulas` | — | Repeatable formula expressions |
| `attribution` | `--month` | previous month | ISO `YYYY-MM` (or RFC3339): `start_month`, and `end_month` unless `--end-month` overrides it. Defaults to the previous UTC month — the newest month with published attribution data |
| | `--end-month` | `--month` | End of the range, for multi-month queries |
| | `--fields` | `*` | Comma-separated cost fields, e.g. `infra_host_on_demand_cost,infra_host_percentage_in_account`; `*` = all |
| | `--tags` | — | Tag keys to break cost down by (`tag_breakdown_keys`) |
| | `--sort-name` / `--sort-direction` | — | Billing dimension to sort by + `asc`/`desc` (always sorted by total cost) |

`attribution` auto-paginates via `meta.pagination.next_record_id`, capped at 10 pages, with a **5-second sleep between page requests** per Datadog's stated rate-limit guidance for this endpoint. Data for a given month becomes available no later than the 19th of the following month; parent-level organizations only, not available on the Government (US1-FED) site. Use `dimensions`'s output to build `--fields` (`<dimension>_on_demand_cost`, `<dimension>_percentage_in_account`, etc.).

### usage

```bash
ddx usage summary --from 30d
ddx usage hourly --families logs --from 24h
ddx usage hourly --families apm,rum --from 7d --to now
ddx usage estimated
ddx usage estimated --month 2026-07 --view sub-org
ddx usage historical --month 2026-05
ddx usage projected
ddx usage billable --month 2026-06
ddx usage top-metrics --month 2026-06 --names my.custom.metric,other.metric
ddx usage logs-by-index --from 24h --index-name main,pci
ddx usage billing-dimensions --month 2026-06 --view all
```

**API**: `GET /api/v2/usage/hourly_usage` (hourly), `GET /api/v2/usage/estimated_cost` (estimated), `GET /api/v2/usage/historical_cost` (historical), `GET /api/v2/usage/projected_cost` (projected), `GET /api/v1/usage/billable-summary` (billable), `GET /api/v1/usage/top_avg_metrics` (top-metrics), `GET /api/v1/usage/logs_by_index` (logs-by-index), `GET /api/v2/usage/billing_dimension_mapping` (billing-dimensions)

| Command | Flag | Default | Description |
|---------|------|---------|-------------|
| `hourly` | `--families` | — | Comma-separated product families, e.g. `"logs,apm"` or `"all"` (required) |
| | `--next` | — | Resume from a `meta.pagination.next_record_id` cursor instead of the first page |
| `estimated`/`historical`/`billable`/`top-metrics`/`billing-dimensions` | `--month` | current UTC month | ISO `YYYY-MM` (or RFC3339); `historical`'s `--month` is required (no default) |
| `estimated`/`historical`/`projected`/`billing-dimensions` | `--view` | `summary`/`active` | `summary`/`sub-org` (cost endpoints) or `active`/`all` (billing-dimensions) |
| `top-metrics` | `--names` | — | Comma-separated metric names to filter to |
| `logs-by-index` | `--index-name` | — | Comma-separated log index names to filter to |

Operational timing notes, all live-probed 2026-07-20 (EU org): **hourly** usage data lags — recent hours may be incomplete. **estimated** cost covers only the current and previous month, delayed up to 72h, and requires exactly one of `start_month`/`start_date` (a bare request with neither 400s) — ddx supplies the current month by default. **historical** cost for a given month isn't available until no later than the 16th of the following month, and is parent-level-organization only. **projected** cost has no month parameter at all — it always reflects the current month, becoming available around the 12th. **billable** and **billing-dimensions** are parent-level-organization only; billing-dimensions mapping data updates on a monthly cadence. `hourly` auto-paginates (`meta.pagination.next_record_id`, capped at 10 pages of up to 500 records); `top-metrics` fetches a single page sized by `--limit` (API max 5000, default 500) — raise `--limit` rather than expecting auto-pagination.

### service-catalog, scorecards, investigations, network

```bash
ddx service-catalog list
ddx scorecards list --name Observability --limit 20
ddx scorecards rules --enabled-only --custom-only
ddx scorecards rules --name "Code Repos Defined"
ddx investigations list
ddx investigations list --monitor-id 12345678 --limit 10
ddx network devices
ddx network connections --from 1h
ddx network connections --group-by client_service,server_service --from 4h
ddx network dns --group-by network.dns_query --from 4h
```

All three of `scorecards`, `investigations`, and `network` were repointed off dead endpoints this release (live-probed 2026-07-20, EU org):

| Command | Old (404) | New |
|---------|-----------|-----|
| `scorecards list` | `api/v2/scorecards` | `GET api/v2/scorecard/scorecards` |
| `scorecards rules` | `api/v2/scorecards/rules` | `GET api/v2/scorecard/rules` (optionally merges parent scorecard via `?include=scorecard`) |
| `investigations list`/`get` | `api/v2/security_monitoring/investigations` | `GET api/v2/bits-ai/investigations` — this is **Bits AI** investigations, not Security Monitoring; preview/x-unstable API |
| `network connections`/`dns` | `api/v2/network/flows` (`network flows` command **removed**) | `GET api/v2/network/connections/aggregate` / `GET api/v2/network/dns/aggregate` (Cloud Network Monitoring) |

**scorecards list flags:** `--id`, `--name`, `--description` (all partial/exact match filters).

**scorecards rules flags:** `--rule-id`, `--name`, `--description`, `--enabled-only`, `--custom-only`, `--include-scorecard` (default true).

**investigations list flags:** `--monitor-id` (`filter[monitor_id]`, must be an integer). `--limit` drives offset pagination (`page[offset]`/`page[limit]`, max 100/page) — values above 100 fetch additional pages.

**network connections/dns flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--group-by` | `client_service,server_service` (connections) / `network.dns_query` (dns) | Comma-separated fields to group by (max 10) |
| `--query` | — | Free-form query, e.g. `"(client_team:networks OR client_team:platform) AND server_service:x"` — takes precedence over `--tags` |
| `--tags` | — | Comma-separated tags to filter by (ignored if `--query` is set) |
| `--network-limit` | 100 | Max rows returned (server-side cap, max 7500) |

**Cloud Network Monitoring (CNM) must be enabled on the org** for `network connections`/`network dns` — without it Datadog returns a bare `400 Bad Request` (not 403) even for an otherwise spec-valid request.

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

An empty `--jq` result against non-empty data prints a 5-line gjson cheat-sheet to stderr once per process (`internal/output/filter.go`, `helpersFilterCheatSheet`) — covers `#.field`, `#.{a:a,b:b}`, `#(x=="y").id`, and `data.0.attributes`. Never printed to stdout, and never fires for a legitimately empty `[]`/`{}`/`null` input (nothing there to plausibly match against).

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | API/network error |
| 2 | Auth error (401/403) |
| 3 | Validation (400) |
| 4 | Not found (404) |
| 5 | Rate limited (429) |
| 6 | Write refused — confirmation required (`write_locked`); re-run with `--yes` |

Fleet-canonical table (`clicore/cierrors.ExitCodeFor`).

## Architecture

```
cmd/ddx/main.go              Entry point, version injection, exit codes
internal/
  client/client.go            HTTP client, DD auth headers, retries, dual pagination
  client/errors.go            APIError with ExitCode(), hints
  commands/root.go            Root command, global flags, getClient()
  commands/confirm.go         requireConfirm()/dryRun() — the write-safety gate
  commands/paginate.go         paginateOffset() — shared offset-pagination loop (v1 start/count, v2 page[offset]/page[size])
  commands/querysplit.go       splitQueriesTopLevel() — comma-split outside {}/()/quotes, shared by metrics/cost query --queries/--formulas
  commands/logs_config.go      logs metrics/archives/destinations config groups (new)
  commands/metrics_cardinality.go  volumes/estimate/tag-cardinalities/tags/tag-rules (new)
  commands/metrics_scalar.go   metrics scalar (new)
  commands/teams.go            teams list/get — the api/v2/team surface (new)
  commands/*.go               One file per command group (44 files)
  config/config.go            TOML config, multi-project, credential resolution
  output/output.go            JSON/table dispatcher, TTY detection
  output/table.go             go-pretty table rendering, column definitions
  output/filter.go            gjson --jq filter + empty-result cheat-sheet hint
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
