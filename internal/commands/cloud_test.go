package commands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nicolasacchi/ddx/internal/client"
)

type cloudSrv struct {
	mu    sync.Mutex
	calls []string
}

// newCloudServer answers 200 only to GET on the given paths, 405 to any other
// method and 404 to unknown paths, mimicking the Datadog API.
func newCloudServer(t *testing.T, ok map[string]bool, post map[string]bool) (*client.Client, *cloudSrv) {
	t.Helper()
	s := &cloudSrv{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls = append(s.calls, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && ok[r.URL.Path]:
			w.Write([]byte(`{"accounts":[]}`))
		case post[r.URL.Path] && r.Method != http.MethodPost:
			http.Error(w, `{"errors":["method not allowed"]}`, http.StatusMethodNotAllowed)
		default:
			http.Error(w, `{"errors":["not found"]}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := client.New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	return c, s
}

func TestCloudAzureUsesListEndpoint(t *testing.T) {
	// host_filters is POST-only: a GET there is a 405, which was the bug.
	c, s := newCloudServer(t,
		map[string]bool{"/" + cloudAzureV1: true},
		map[string]bool{"/api/v1/integration/azure/host_filters": true})
	if _, err := cloudGet(context.Background(), c, cloudAzureV1, ""); err != nil {
		t.Fatalf("azure list: %v", err)
	}
	if len(s.calls) != 1 || s.calls[0] != "GET /api/v1/integration/azure" {
		t.Fatalf("calls = %v", s.calls)
	}
}

func TestCloudV2Preferred(t *testing.T) {
	c, s := newCloudServer(t, map[string]bool{"/" + cloudAWSv2: true}, nil)
	if _, err := cloudGet(context.Background(), c, cloudAWSv2, cloudAWSv1); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 1 {
		t.Fatalf("v1 must not be called when v2 works: %v", s.calls)
	}
}

func TestCloudFallsBackToV1On404(t *testing.T) {
	c, s := newCloudServer(t, map[string]bool{"/" + cloudGCPv1: true}, nil)
	if _, err := cloudGet(context.Background(), c, cloudGCPv2, cloudGCPv1); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 2 || s.calls[1] != "GET /"+cloudGCPv1 {
		t.Fatalf("calls = %v", s.calls)
	}
}

func TestCloudNoFallbackOnOtherErrors(t *testing.T) {
	c, s := newCloudServer(t, nil, map[string]bool{"/" + cloudAWSv2: true})
	if _, err := cloudGet(context.Background(), c, cloudAWSv2, cloudAWSv1); err == nil {
		t.Fatal("expected 405 error")
	}
	if len(s.calls) != 1 {
		t.Fatalf("405 must not trigger fallback: %v", s.calls)
	}
}
