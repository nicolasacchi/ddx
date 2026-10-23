package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/nicolasacchi/ddx/internal/ratelimit"
)

func TestPaListBody(t *testing.T) {
	body := paListBody(paListSpec{
		DataSource: "product_analytics", Query: "@type:action", Columns: []string{"@action.name", "@session.id"},
		Limit: 1000, Desc: false, From: 100, To: 200,
	})
	b, _ := json.Marshal(body)
	want := `{"data":{"attributes":{"from":100,"query":{"columns":["@action.name","@session.id"],"limit":1000,"query":{"data_source":"product_analytics","search":{"query":"@type:action"}},"sort":{"facet":"timestamp","order":"asc"}},"to":200},"type":"formula_analytics_extended_list_request"}}`
	if string(b) != want {
		t.Fatalf("list body\n got %s\nwant %s", b, want)
	}
}

func TestPaBustQuery(t *testing.T) {
	old := paNonce
	paNonce = func() string { return "abc123" }
	defer func() { paNonce = old }()
	if got := paBustQuery("@type:action OR @type:view", paNonceIf(true)); got != "(@type:action OR @type:view) -@ddx_nonce:abc123" {
		t.Fatalf("bust query = %q", got)
	}
	if got := paBustQuery("*", paNonceIf(false)); got != "*" {
		t.Fatalf("no-bust query = %q", got)
	}
	b, _ := json.Marshal(paListBody(paListSpec{DataSource: "product_analytics", Query: "*", Columns: []string{"a"}, Limit: 10, Bust: true}))
	if !strings.Contains(string(b), `"(*) -@ddx_nonce:abc123"`) {
		t.Fatalf("list body not busted: %s", b)
	}
	b, _ = json.Marshal(paAnalyticsBody("product_analytics", "*", true, "count", "", nil, 0, 1, 2))
	if !strings.Contains(string(b), `"(*) -@ddx_nonce:abc123"`) {
		t.Fatalf("analytics body not busted: %s", b)
	}
}

func TestPaAnalyticsBodyComputeIsSiblingOfQuery(t *testing.T) {
	body := paAnalyticsBody("product_analytics", "@type:view", false, "cardinality", "@session.id",
		[]paGroup{{"@type", 20}}, 0, 1, 2)
	attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
	q := attrs["query"].(map[string]any)
	if _, ok := q["compute"]; !ok {
		t.Fatal("compute must be a sibling of the inner query")
	}
	if _, ok := q["query"].(map[string]any)["compute"]; ok {
		t.Fatal("compute must not be inside the inner query")
	}
	wantCompute := map[string]any{"aggregation": "cardinality", "metric": "@session.id"}
	if !reflect.DeepEqual(q["compute"], wantCompute) {
		t.Fatalf("compute = %#v", q["compute"])
	}
	if got := q["group_by"].([]map[string]any); got[0]["facet"] != "@type" || got[0]["limit"] != 20 {
		t.Fatalf("group_by = %#v", got)
	}
	if body["data"].(map[string]any)["type"] != "formula_analytics_extended_request" {
		t.Fatal("wrong request type")
	}

	ts := paAnalyticsBody("product_analytics", "*", false, "count", "", nil, 3600000, 1, 2)
	c := ts["data"].(map[string]any)["attributes"].(map[string]any)["query"].(map[string]any)["compute"].(map[string]any)
	if c["interval"] != int64(3600000) || c["aggregation"] != "count" {
		t.Fatalf("timeseries compute = %#v", c)
	}
	if _, ok := c["metric"]; ok {
		t.Fatal("count must not carry a metric")
	}
}

func TestPaParseCompute(t *testing.T) {
	cases := []struct {
		in, agg, metric string
		wantErr         bool
	}{
		{"count", "count", "", false},
		{"", "count", "", false},
		{"cardinality(@f)", "cardinality", "@f", false},
		{"sum(@a.b)", "sum", "@a.b", false},
		{"avg(@x)", "avg", "@x", false},
		{"p95(@x)", "", "", true},
		{"sum()", "", "", true},
		{"sum", "", "", true},
	}
	for _, tc := range cases {
		agg, metric, err := paParseCompute(tc.in)
		if (err != nil) != tc.wantErr || agg != tc.agg || metric != tc.metric {
			t.Errorf("paParseCompute(%q) = %q,%q,%v", tc.in, agg, metric, err)
		}
	}
}

func TestPaParseGroupBysAndInterval(t *testing.T) {
	g, err := paParseGroupBys([]string{"@type", "@view.name:50"})
	if err != nil || !reflect.DeepEqual(g, []paGroup{{"@type", 20}, {"@view.name", 50}}) {
		t.Fatalf("groups = %v err=%v", g, err)
	}
	if _, err := paParseGroupBys([]string{"@x:0"}); err == nil {
		t.Fatal("limit 0 must fail")
	}
	for in, want := range map[string]time.Duration{"1h": time.Hour, "30m": 30 * time.Minute, "1d": 24 * time.Hour, "2w": 14 * 24 * time.Hour} {
		if d, err := paParseInterval(in); err != nil || d != want {
			t.Errorf("paParseInterval(%q) = %v, %v", in, d, err)
		}
	}
	if _, err := paParseInterval("fast"); err == nil {
		t.Fatal("garbage interval must fail")
	}
}

// ---- fake Product Analytics server -------------------------------------

type fakeEvent struct {
	Ts   int64
	Name string
}

type fakePA struct {
	events   []fakeEvent // any order
	requests []map[string]any
	calls    atomic.Int32

	// cache emulates the real API's response cache: keyed on the query shape
	// plus the MINUTE-rounded from/to (the search string is part of the shape,
	// so a nonce makes every request unique).
	cache  bool
	cached map[string][]byte
}

// handler emulates analytics/list: from/to FLOORED to whole seconds (as the
// real API does), from inclusive, to exclusive, sort object, limit cap, no
// cursor, 500 on an empty (sub-second) window.
func (f *fakePA) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		f.requests = append(f.requests, req)
		attrs := req["data"].(map[string]any)["attributes"].(map[string]any)
		q := attrs["query"].(map[string]any)
		from, to := paFloorSec(int64(attrs["from"].(float64))), paFloorSec(int64(attrs["to"].(float64)))
		limit := int(q["limit"].(float64))
		if from >= to {
			w.WriteHeader(500)
			w.Write([]byte(`{"errors":[{"title":"Generic Error","detail":"failed to execute query"}]}`))
			return
		}
		cacheKey := ""
		if f.cache {
			search := q["query"].(map[string]any)["search"].(map[string]any)["query"]
			cacheKey = fmt.Sprintf("%v|%v|%v|%d|%d", search, q["sort"], q["columns"], from/60000, to/60000)
			if f.cached == nil {
				f.cached = map[string][]byte{}
			}
			if b, ok := f.cached[cacheKey]; ok {
				w.Write(b)
				return
			}
		}
		sortObj, ok := q["sort"].(map[string]any)
		if !ok {
			w.WriteHeader(400)
			w.Write([]byte(`{"errors":[{"detail":"sort must be an object"}]}`))
			return
		}
		if limit > 1000 {
			w.WriteHeader(400)
			w.Write([]byte(`{"errors":[{"detail":"limit must be lte 1000"}]}`))
			return
		}
		var in []fakeEvent
		for _, e := range f.events {
			if e.Ts >= from && e.Ts < to {
				in = append(in, e)
			}
		}
		desc := sortObj["order"] == "desc"
		sort.SliceStable(in, func(i, j int) bool {
			if desc {
				return in[i].Ts > in[j].Ts
			}
			return in[i].Ts < in[j].Ts
		})
		total := len(in)
		if len(in) > limit {
			in = in[:limit]
		}
		recs := make([]map[string]any, len(in))
		for i, e := range in {
			recs[i] = map[string]any{"@action.name": e.Name, "timestamp": e.Ts}
		}
		out, _ := json.Marshal(map[string]any{
			"data": map[string]any{"attributes": map[string]any{"records": recs, "total_count": total}},
			"meta": map[string]any{"status": "done"},
		})
		if f.cache {
			f.cached[cacheKey] = out
		}
		w.Write(out)
	}
}

func paTestEnv(t *testing.T, f *fakePA) (*client.Client, *ratelimit.Limiter) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	c := client.New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	return c, &ratelimit.Limiter{Bucket: "pa-test", Dir: t.TempDir()}
}

const paT0 = int64(1_000_000) // second-aligned base for fake data

// tiedEvents builds ~1.4 events/second over 42s. Some timestamps carry
// same-ms siblings, and every second holds several rows, so small pages end
// mid-second and mid-tie. Two events are byte-identical (same ms, same
// record) to prove multiplicity is preserved.
func tiedEvents() []fakeEvent {
	var ev []fakeEvent
	for i := int64(0); i < 60; i++ {
		ts := paT0 + i*700
		ev = append(ev, fakeEvent{ts, fmt.Sprintf("e%d", i)})
		if i%4 == 0 { // same-ms sibling, different record
			ev = append(ev, fakeEvent{ts, fmt.Sprintf("sib%d", i)})
		}
	}
	ev = append(ev, fakeEvent{paT0 + 10*700, "twin"}, fakeEvent{paT0 + 10*700, "twin"})
	return ev
}

func eventKeys(ev []fakeEvent) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = fmt.Sprintf("%d/%s", e.Ts, e.Name)
	}
	sort.Strings(out)
	return out
}

func runPaginate(t *testing.T, f *fakePA, desc bool, pageSize, maxRows int, from, to int64) ([]fakeEvent, paStats, error) {
	t.Helper()
	c, rl := paTestEnv(t, f)
	var got []fakeEvent
	st, err := paPaginate(context.Background(), paPagerOpts{
		From: from, To: to, Desc: desc, PageSize: pageSize, MaxRows: maxRows,
		Fetch: func(ctx context.Context, fr, tt int64) (paPage, error) {
			return paFetchList(ctx, c, rl, paListSpec{DataSource: "product_analytics", Query: "*", Columns: []string{"@action.name"}, Limit: pageSize, Desc: desc, From: fr, To: tt})
		},
		Emit: func(r map[string]any) error {
			ts, _ := paTimestamp(r)
			got = append(got, fakeEvent{ts, r["@action.name"].(string)})
			return nil
		},
	})
	return got, st, err
}

func TestPaPaginateBoundaryTiesEveryRowExactlyOnce(t *testing.T) {
	for _, desc := range []bool{true, false} {
		for _, ps := range []int{6, 7, 11, 1000} {
			t.Run(fmt.Sprintf("desc=%v/page=%d", desc, ps), func(t *testing.T) {
				f := &fakePA{events: tiedEvents()}
				got, st, err := runPaginate(t, f, desc, ps, 0, paT0-5000, paT0+50000)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(eventKeys(got), eventKeys(f.events)) {
					t.Fatalf("rows differ\n got %v\nwant %v", eventKeys(got), eventKeys(f.events))
				}
				if st.Rows != len(f.events) || st.Total != len(f.events) || st.Truncated {
					t.Fatalf("stats = %+v", st)
				}
				// monotonic in the requested order
				for i := 1; i < len(got); i++ {
					if (desc && got[i].Ts > got[i-1].Ts) || (!desc && got[i].Ts < got[i-1].Ts) {
						t.Fatalf("order violated at %d: %v then %v", i, got[i-1], got[i])
					}
				}
			})
		}
	}
}

func TestPaPaginateDedupesBoundaryRepeats(t *testing.T) {
	f := &fakePA{events: tiedEvents()}
	_, st, err := runPaginate(t, f, true, 6, 0, paT0-5000, paT0+50000)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dupes == 0 || st.Pages < 2 {
		t.Fatalf("expected boundary repeats to be removed across pages, stats=%+v", st)
	}
}

func TestPaPaginateWindowAdvanceUsesBoundarySecond(t *testing.T) {
	attr := func(f *fakePA, i int, k string) int64 {
		return int64(f.requests[i]["data"].(map[string]any)["attributes"].(map[string]any)[k].(float64))
	}
	from, to := paT0-5000, paT0+50000

	f := &fakePA{events: tiedEvents()}
	if _, _, err := runPaginate(t, f, true, 6, 0, from, to); err != nil {
		t.Fatal(err)
	}
	// desc: from fixed, to shrinks to a whole-second edge (end of the oldest row's second)
	if attr(f, 0, "to") != to || attr(f, 0, "from") != from {
		t.Fatalf("first window wrong: %d..%d", attr(f, 0, "from"), attr(f, 0, "to"))
	}
	for i := 1; i < len(f.requests); i++ {
		if attr(f, i, "from") != from || attr(f, i, "to") >= attr(f, i-1, "to") || attr(f, i, "to")%1000 != 0 {
			t.Fatalf("desc window %d not advanced on a second edge: %d..%d", i, attr(f, i, "from"), attr(f, i, "to"))
		}
	}

	f2 := &fakePA{events: tiedEvents()}
	runPaginate(t, f2, false, 6, 0, from, to)
	for i := 1; i < len(f2.requests); i++ {
		if attr(f2, i, "to") != to || attr(f2, i, "from") <= attr(f2, i-1, "from") || attr(f2, i, "from")%1000 != 0 {
			t.Fatalf("asc window %d not advanced on a second edge: %d..%d", i, attr(f2, i, "from"), attr(f2, i, "to"))
		}
	}
}

func TestPaPaginateRejectsOutOfWindowRows(t *testing.T) {
	_, err := paPaginate(context.Background(), paPagerOpts{
		From: 10000, To: 20000, Desc: true, PageSize: 5,
		Fetch: func(ctx context.Context, f, t int64) (paPage, error) {
			return paPage{Records: []map[string]any{{"timestamp": json.Number("25000")}}, Total: 1}, nil
		},
		Emit: func(map[string]any) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "outside the requested window") {
		t.Fatalf("err = %v", err)
	}
}

// The real API serves a cached response for the same query shape and
// minute-rounded window. Without a nonce the pager must fail loudly (never
// return a wrong dataset); with the nonce it must return everything.
func TestPaCacheBust(t *testing.T) {
	var ev []fakeEvent
	for i := int64(0); i < 40; i++ { // 40 rows in ~20s, all inside one minute
		ev = append(ev, fakeEvent{paT0 + i*500, fmt.Sprintf("e%d", i)})
	}
	run := func(bust bool) ([]fakeEvent, error) {
		f := &fakePA{events: ev, cache: true}
		c, rl := paTestEnv(t, f)
		var got []fakeEvent
		_, err := paPaginate(context.Background(), paPagerOpts{
			From: paT0, To: paT0 + 30000, Desc: true, PageSize: 10,
			Fetch: func(ctx context.Context, fr, tt int64) (paPage, error) {
				return paFetchList(ctx, c, rl, paListSpec{DataSource: "product_analytics", Query: "@type:action", Columns: []string{"@action.name"}, Limit: 10, Desc: true, From: fr, To: tt, Bust: bust})
			},
			Emit: func(r map[string]any) error {
				ts, _ := paTimestamp(r)
				got = append(got, fakeEvent{ts, r["@action.name"].(string)})
				return nil
			},
		})
		return got, err
	}
	if got, err := run(true); err != nil || len(got) != len(ev) {
		t.Fatalf("busted: rows=%d err=%v", len(got), err)
	}
	if _, err := run(false); err == nil {
		t.Fatal("unbusted run against a caching API must not silently succeed")
	}
}

func TestPaPaginateNoProgressFailsLoudly(t *testing.T) {
	var ev []fakeEvent
	for i := 0; i < 8; i++ { // 8 distinct rows inside one second, page size 5
		ev = append(ev, fakeEvent{paT0 + 7000 + int64(i)*10, fmt.Sprintf("r%d", i)})
	}
	for _, desc := range []bool{true, false} {
		f := &fakePA{events: ev}
		_, _, err := runPaginate(t, f, desc, 6, 0, paT0-5000, paT0+50000)
		if err == nil || !strings.Contains(err.Error(), "cannot page") {
			t.Fatalf("desc=%v err=%v, want a no-progress error", desc, err)
		}
		if f.calls.Load() > 2 {
			t.Fatalf("desc=%v looped: %d calls", desc, f.calls.Load())
		}
	}
}

func TestPaPaginateMaxRows(t *testing.T) {
	f := &fakePA{events: tiedEvents()}
	got, st, err := runPaginate(t, f, true, 6, 8, paT0-5000, paT0+50000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 || !st.Truncated || st.Total != len(f.events) {
		t.Fatalf("rows=%d stats=%+v", len(got), st)
	}
	// cap larger than data: not truncated
	f = &fakePA{events: tiedEvents()}
	got, st, _ = runPaginate(t, f, true, 6, 1000, paT0-5000, paT0+50000)
	if len(got) != len(f.events) || st.Truncated {
		t.Fatalf("rows=%d stats=%+v", len(got), st)
	}
}

func TestPaPaginateEmptyWindow(t *testing.T) {
	f := &fakePA{}
	got, st, err := runPaginate(t, f, true, 6, 0, paT0-5000, paT0+50000)
	if err != nil || len(got) != 0 || st.Rows != 0 {
		t.Fatalf("got=%v st=%+v err=%v", got, st, err)
	}
}

func TestPaPaginateRequiresTimestamp(t *testing.T) {
	_, err := paTimestamp(map[string]any{"@x": 1})
	if err == nil {
		t.Fatal("want error for record without timestamp")
	}
}

func TestPaFetchListSendsSortObjectAndLimit(t *testing.T) {
	f := &fakePA{events: tiedEvents()}
	c, rl := paTestEnv(t, f)
	if _, err := paFetchList(context.Background(), c, rl, paListSpec{DataSource: "product_analytics", Query: "*", Columns: []string{"a"}, Limit: 2000, From: 0, To: 5000}); err == nil {
		t.Fatal("limit > 1000 must surface the API error")
	}
	q := f.requests[0]["data"].(map[string]any)["attributes"].(map[string]any)["query"].(map[string]any)
	if _, ok := q["sort"].(map[string]any); !ok {
		t.Fatalf("sort must be an object, got %#v", q["sort"])
	}
}

func TestPaPostStatusHandling(t *testing.T) {
	old := paRunningDelay
	paRunningDelay = time.Millisecond
	defer func() { paRunningDelay = old }()

	var calls atomic.Int32
	status := "running"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		s := status
		if status == "running" && n >= 3 {
			s = "done"
		}
		fmt.Fprintf(w, `{"data":{},"meta":{"status":%q,"request_id":"rid"}}`, s)
	}))
	defer srv.Close()
	c := client.New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	rl := &ratelimit.Limiter{Bucket: "s", Dir: t.TempDir()}

	if _, err := paPost(context.Background(), c, rl, "x", func() any { return map[string]any{} }); err != nil || calls.Load() != 3 {
		t.Fatalf("running->done: err=%v calls=%d", err, calls.Load())
	}

	status, calls = "timeout", atomic.Int32{}
	_, err := paPost(context.Background(), c, rl, "x", func() any { return map[string]any{} })
	ae, ok := err.(*client.APIError)
	if !ok || ae.ExitCode() != 7 || calls.Load() != 1 {
		t.Fatalf("timeout: err=%v calls=%d, want exit 7 without retry", err, calls.Load())
	}
}

func TestPaPostRunningForeverFails(t *testing.T) {
	old := paRunningDelay
	paRunningDelay = time.Millisecond
	defer func() { paRunningDelay = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{},"meta":{"status":"running"}}`))
	}))
	defer srv.Close()
	c := client.New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	_, err := paPost(context.Background(), c, &ratelimit.Limiter{Bucket: "s", Dir: t.TempDir()}, "x", func() any { return map[string]any{} })
	if ae, ok := err.(*client.APIError); !ok || ae.ExitCode() != 7 {
		t.Fatalf("err=%v, want async_timeout", err)
	}
}

func TestPaScalarRows(t *testing.T) {
	raw := json.RawMessage(`{"data":{"attributes":{"columns":[
		{"name":"@type","type":"group","values":[["action"],["view"],["session"]]},
		{"name":"query","type":"number","values":[31627,30576,2597]}]}}}`)
	rows, err := paScalarRows(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rows)
	want := `[{"@type":"action","value":31627},{"@type":"view","value":30576},{"@type":"session","value":2597}]`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

func TestPaTimeseriesRows(t *testing.T) {
	raw := json.RawMessage(`{"data":{"attributes":{
		"times":[1792720800000,1792724400000],
		"series":[{"group_tags":["@type:action"],"query_index":0},{"group_tags":["@type:view"],"query_index":0}],
		"values":[[254,null],[7,8]]}}}`)
	rows, err := paTimeseriesRows(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %v", rows)
	}
	b, _ := json.Marshal(rows[0])
	if string(b) != `{"@type":"action","time":"2026-10-23T02:00:00Z","time_ms":1792720800000,"value":254}` {
		t.Fatalf("row0 = %s", b)
	}
	if rows[1]["value"] != nil || rows[3]["@type"] != "view" {
		t.Fatalf("rows = %v", rows)
	}
	// ungrouped
	raw = json.RawMessage(`{"data":{"attributes":{"times":[1],"series":[{"group_tags":[]}],"values":[[5]]}}}`)
	if rows, err = paTimeseriesRows(raw); err != nil || len(rows) != 1 || rows[0]["value"] == nil {
		t.Fatalf("ungrouped rows=%v err=%v", rows, err)
	}
}

func TestPaWriters(t *testing.T) {
	recs := []map[string]any{
		{"a": "x,y", "b": json.Number("12"), "timestamp": json.Number("5")},
		{"a": nil, "b": []any{"q"}, "timestamp": json.Number("6")},
	}
	run := func(format string) string {
		var buf bytes.Buffer
		w, _ := paNewRowWriter(format, &buf, []string{"a", "b"})
		for _, r := range recs {
			if err := w.Write(r); err != nil {
				t.Fatal(err)
			}
		}
		w.Close()
		return buf.String()
	}
	if got := run("csv"); got != "a,b,timestamp\n\"x,y\",12,5\n,\"[\"\"q\"\"]\",6\n" {
		t.Fatalf("csv = %q", got)
	}
	if got := run("jsonl"); strings.Count(got, "\n") != 2 {
		t.Fatalf("jsonl = %q", got)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(run("json")), &arr); err != nil || len(arr) != 2 {
		t.Fatalf("json array invalid: %v", err)
	}
	var empty bytes.Buffer
	w, _ := paNewRowWriter("json", &empty, []string{"a"})
	w.Close()
	if strings.TrimSpace(empty.String()) != "[]" {
		t.Fatalf("empty json = %q", empty.String())
	}
}

func TestPaWriteRowsCSVOrdering(t *testing.T) {
	var buf bytes.Buffer
	rows := []map[string]any{{"value": 1, "@type": "a", "time_ms": int64(5), "time": "t"}}
	if err := paWriteRowsCSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "time,time_ms,@type,value\nt,5,a,1\n" {
		t.Fatalf("csv = %q", buf.String())
	}
}

func TestPaCheckWindow(t *testing.T) {
	if err := paCheckWindow(10, 11); err != nil {
		t.Fatal(err)
	}
	if err := paCheckWindow(10, 10); err == nil || !strings.Contains(err.Error(), "whole second") {
		t.Fatalf("equal window: %v", err)
	}
	if err := paCheckWindow(11, 10); err == nil || !strings.Contains(err.Error(), "after --to") {
		t.Fatalf("inverted window: %v", err)
	}
}

func TestPaCSVWriterDedupesColumnsAndFlushes(t *testing.T) {
	var buf bytes.Buffer
	rw, err := paNewRowWriter("csv", &buf, []string{"@a", "timestamp", "@a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rw.Write(map[string]any{"@a": "x", "timestamp": json.Number("5")}); err != nil {
		t.Fatal(err)
	}
	if err := rw.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "@a,timestamp\nx,5\n" {
		t.Fatalf("after Flush: %q", got)
	}
}
