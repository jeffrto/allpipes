package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeGitLab struct {
	mu        sync.Mutex
	pipelines []Pipeline
	details   atomic.Int32
}

func (f *fakeGitLab) set(p ...Pipeline) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pipelines = p
}

func (f *fakeGitLab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v4")
	switch {
	case path == "/groups/acme/projects":
		json.NewEncoder(w).Encode([]Project{
			{ID: 1, Name: "api", PathWithNamespace: "acme/api"},
			{ID: 2, Name: "docs", PathWithNamespace: "acme/docs"},
		})
	case path == "/projects/1/pipelines":
		json.NewEncoder(w).Encode(f.pipelines)
	case strings.HasPrefix(path, "/projects/1/pipelines/"):
		f.details.Add(1)
		for _, p := range f.pipelines {
			if path == fmt.Sprintf("/projects/1/pipelines/%d", p.ID) {
				duration := 42.0
				p.Duration = &duration
				p.User = &User{Name: "Ada"}
				json.NewEncoder(w).Encode(p)
				return
			}
		}
		http.NotFound(w, r)
	case path == "/projects/2/pipelines":
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	default:
		http.NotFound(w, r)
	}
}

func TestMonitorPoll(t *testing.T) {
	fake := &fakeGitLab{}
	server := httptest.NewServer(fake)
	defer server.Close()

	ctx := context.Background()
	m := NewMonitor(NewClient(server.URL, "token"), MonitorOptions{Group: Group{FullPath: "acme"}})
	if err := m.loadProjects(ctx); err != nil {
		t.Fatal(err)
	}

	t0 := time.Now().Add(-time.Minute)
	fake.set(Pipeline{ID: 10, Status: "running", Ref: "main", CreatedAt: t0, UpdatedAt: t0})
	if _, err := m.poll(ctx, true); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	snap := m.Snapshot()
	if !snap.Ready || len(snap.Pipelines) != 1 || len(snap.Events) != 0 {
		t.Fatalf("first poll: ready=%v pipelines=%d events=%d", snap.Ready, len(snap.Pipelines), len(snap.Events))
	}
	if p := snap.Pipelines[0]; p.User == nil || p.ProjectPath != "acme/api" {
		t.Fatalf("pipeline not enriched: %+v", p)
	}

	if _, err := m.poll(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := fake.details.Load(); got != 1 {
		t.Fatalf("unchanged pipeline refetched details: %d requests", got)
	}

	t1 := time.Now()
	fake.set(Pipeline{ID: 10, Status: "failed", Ref: "main", CreatedAt: t0, UpdatedAt: t1})
	m.poll(ctx, false)
	snap = m.Snapshot()
	if len(snap.Events) != 1 || snap.Events[0].PreviousStatus != "running" || snap.Events[0].NewStatus != "failed" {
		t.Fatalf("expected running→failed event, got %+v", snap.Events)
	}
	if snap.Stats.Failed != 1 || snap.Stats.Active != 0 {
		t.Fatalf("stats not updated: %+v", snap.Stats)
	}

	t2 := time.Now().Add(time.Second)
	fake.set(Pipeline{ID: 11, Status: "success", Ref: "main", CreatedAt: t2, UpdatedAt: t2})
	m.poll(ctx, false)
	snap = m.Snapshot()
	if len(snap.Pipelines) != 1 || snap.Pipelines[0].ID != 11 {
		t.Fatalf("stale pipelines were kept: %+v", snap.Pipelines)
	}
	if snap.Events[0].Seq != 2 || snap.Events[0].NewStatus != "success" {
		t.Fatalf("expected new success event first, got %+v", snap.Events[0])
	}
	if snap.Error != "" {
		t.Fatalf("403 on a project without CI should not be an error: %s", snap.Error)
	}
}
