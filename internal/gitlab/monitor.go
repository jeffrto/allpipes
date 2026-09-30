package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxEvents           = 100
	projectRefreshEvery = 5 * time.Minute
	maxBackoff          = 8
	hotWindow           = 15 * time.Minute
	idleEvery           = 6
)

var errSkipped = errors.New("skipped while rate limited")

type MonitorOptions struct {
	Group               Group
	IncludeSubgroups    bool
	PollInterval        time.Duration
	PipelinesPerProject int
	Concurrency         int
}

type Monitor struct {
	client *Client
	opts   MonitorOptions

	mu        sync.RWMutex
	projects  []Project
	pipelines map[int64]Pipeline
	events    []PipelineEvent
	seq       int64
	stats     MonitorStats
	lastErr   error
	ready     bool
	since     time.Time

	subsMu sync.Mutex
	subs   map[chan MonitorSnapshot]struct{}

	refresh chan struct{}

	// Owned by the Run goroutine.
	round  int
	polled map[int64]bool
}

func NewMonitor(client *Client, opts MonitorOptions) *Monitor {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 10 * time.Second
	}
	if opts.PipelinesPerProject <= 0 {
		opts.PipelinesPerProject = 3
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	return &Monitor{
		client:    client,
		opts:      opts,
		pipelines: make(map[int64]Pipeline),
		subs:      make(map[chan MonitorSnapshot]struct{}),
		refresh:   make(chan struct{}, 1),
		polled:    make(map[int64]bool),
	}
}

func (m *Monitor) Subscribe() chan MonitorSnapshot {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	ch := make(chan MonitorSnapshot, 1)
	m.subs[ch] = struct{}{}
	return ch
}

func (m *Monitor) Unsubscribe(ch chan MonitorSnapshot) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	if _, ok := m.subs[ch]; ok {
		delete(m.subs, ch)
		close(ch)
	}
}

func (m *Monitor) TriggerRefresh() {
	select {
	case m.refresh <- struct{}{}:
	default:
	}
}

func (m *Monitor) Snapshot() MonitorSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pipes := make([]Pipeline, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		pipes = append(pipes, p)
	}
	sort.Slice(pipes, func(i, j int) bool {
		a, b := pipes[i], pipes[j]
		if a.IsActive() != b.IsActive() {
			return a.IsActive()
		}
		if !a.LastActivity().Equal(b.LastActivity()) {
			return a.LastActivity().After(b.LastActivity())
		}
		return a.ID > b.ID
	})

	snap := MonitorSnapshot{
		Group:               m.opts.Group,
		Stats:               m.stats,
		Pipelines:           pipes,
		Events:              append(make([]PipelineEvent, 0, len(m.events)), m.events...),
		Ready:               m.ready,
		PollInterval:        m.opts.PollInterval.Seconds(),
		PipelinesPerProject: m.opts.PipelinesPerProject,
		Timestamp:           time.Now(),
	}
	if m.lastErr != nil {
		snap.Error = m.lastErr.Error()
	}
	return snap
}

func (m *Monitor) Run(ctx context.Context) {
	var projectsLoadedAt time.Time
	backoff := 1
	full := true

	for {
		var projectErr, pollErr error
		if time.Since(projectsLoadedAt) >= projectRefreshEvery {
			if projectErr = m.loadProjects(ctx); projectErr == nil {
				projectsLoadedAt = time.Now()
			}
		}

		if !projectsLoadedAt.IsZero() {
			var limited bool
			limited, pollErr = m.poll(ctx, full)
			full = false
			if limited {
				backoff = min(backoff*2, maxBackoff)
			} else {
				backoff = 1
			}
		}
		if ctx.Err() != nil {
			return
		}

		m.mu.Lock()
		m.lastErr = errors.Join(projectErr, pollErr)
		m.mu.Unlock()
		m.broadcast()

		select {
		case <-ctx.Done():
			return
		case <-m.refresh:
			full = true
		case <-time.After(m.opts.PollInterval * time.Duration(backoff)):
		}
	}
}

func (m *Monitor) loadProjects(ctx context.Context) error {
	projects, err := m.client.ListGroupProjects(ctx, m.opts.Group.FullPath, m.opts.IncludeSubgroups)
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}
	m.mu.Lock()
	m.projects = projects
	m.stats.TotalProjects = len(projects)
	m.mu.Unlock()
	return nil
}

// Idle projects are only fetched every idleEvery rounds to stay under GitLab's rate limits.
func (m *Monitor) poll(ctx context.Context, full bool) (limited bool, err error) {
	m.mu.RLock()
	projects := m.projects
	prev := m.pipelines
	m.mu.RUnlock()

	now := time.Now()
	m.round++
	byProject := make(map[int64][]Pipeline)
	for _, p := range prev {
		byProject[p.ProjectID] = append(byProject[p.ProjectID], p)
	}
	listed := make(map[int64]bool, len(projects))
	var due []Project
	for _, p := range projects {
		listed[p.ID] = true
		if full || m.isHot(p.ID, byProject[p.ID], now) || (m.round+int(p.ID))%idleEvery == 0 {
			due = append(due, p)
		}
	}

	type result struct {
		project   Project
		pipelines []Pipeline
		err       error
	}

	jobs := make(chan Project)
	results := make(chan result, len(due))
	var rateLimited atomic.Bool
	var wg sync.WaitGroup

	for range min(m.opts.Concurrency, len(due)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for project := range jobs {
				if rateLimited.Load() {
					results <- result{project: project, err: errSkipped}
					continue
				}
				pipes, err := m.fetchProject(ctx, project, prev)
				if StatusCode(err) == http.StatusTooManyRequests {
					rateLimited.Store(true)
				}
				results <- result{project, pipes, err}
			}
		}()
	}
	for _, p := range due {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	close(results)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	next := make(map[int64]Pipeline, len(prev))
	fetched := make(map[int64]bool, len(due))
	failed := 0
	var firstErr error
	for r := range results {
		code := StatusCode(r.err)
		switch {
		case r.err == nil:
			for _, p := range r.pipelines {
				next[p.ID] = p
			}
		case code == http.StatusForbidden || code == http.StatusNotFound:
			// CI is disabled or hidden for this project, so it has no pipelines to show.
		default:
			failed++
			if firstErr == nil && !errors.Is(r.err, errSkipped) {
				firstErr = r.err
			}
			continue
		}
		fetched[r.project.ID] = true
		m.polled[r.project.ID] = true
	}
	for id, p := range prev {
		if listed[p.ProjectID] && !fetched[p.ProjectID] {
			next[id] = p
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ready {
		m.recordTransitions(prev, next, now)
	} else {
		m.ready = true
		m.since = now
	}
	m.pipelines = next
	m.stats = computeStats(next, len(projects), now)

	switch {
	case rateLimited.Load():
		err = fmt.Errorf("GitLab is rate limiting requests (429), so %d of %d projects were skipped and polling slows down until it recovers", failed, len(due))
		if !m.client.Authenticated() {
			err = fmt.Errorf("%w. Anonymous requests get a much lower limit: set GITLAB_TOKEN", err)
		}
	case firstErr != nil:
		err = fmt.Errorf("%d of %d projects could not be polled: %w", failed, len(due), firstErr)
	}
	return rateLimited.Load(), err
}

func (m *Monitor) isHot(projectID int64, pipelines []Pipeline, now time.Time) bool {
	if !m.polled[projectID] {
		return true
	}
	for _, p := range pipelines {
		if p.IsActive() || now.Sub(p.LastActivity()) < hotWindow {
			return true
		}
	}
	return false
}

func (m *Monitor) fetchProject(ctx context.Context, project Project, prev map[int64]Pipeline) ([]Pipeline, error) {
	list, err := m.client.ListPipelines(ctx, project.ID, m.opts.PipelinesPerProject)
	if err != nil {
		return nil, err
	}

	out := make([]Pipeline, 0, len(list))
	for _, p := range list {
		if old, ok := prev[p.ID]; ok && old.detailed && old.Status == p.Status && old.UpdatedAt.Equal(p.UpdatedAt) {
			p = old
		} else if full, err := m.client.GetPipeline(ctx, project.ID, p.ID); err == nil {
			p = *full
			p.detailed = true
		} else if StatusCode(err) == http.StatusTooManyRequests {
			return nil, err
		}
		p.ProjectID = project.ID
		p.ProjectName = project.Name
		p.ProjectPath = project.PathWithNamespace
		p.ProjectWebURL = project.WebURL
		out = append(out, p)
	}
	return out, nil
}

func (m *Monitor) recordTransitions(prev, next map[int64]Pipeline, now time.Time) {
	var fresh []PipelineEvent
	for id, p := range next {
		old, seen := prev[id]
		switch {
		case seen && old.Status != p.Status:
			fresh = append(fresh, PipelineEvent{Pipeline: p, PreviousStatus: old.Status, NewStatus: p.Status, Timestamp: now})
		case !seen && p.CreatedAt.After(m.since) && isNotable(p.Category()):
			fresh = append(fresh, PipelineEvent{Pipeline: p, NewStatus: p.Status, Timestamp: now})
		}
	}
	if len(fresh) == 0 {
		return
	}

	sort.Slice(fresh, func(i, j int) bool {
		return fresh[i].Pipeline.UpdatedAt.Before(fresh[j].Pipeline.UpdatedAt)
	})
	for i := range fresh {
		m.seq++
		fresh[i].Seq = m.seq
	}
	for i, j := 0, len(fresh)-1; i < j; i, j = i+1, j-1 {
		fresh[i], fresh[j] = fresh[j], fresh[i]
	}
	m.events = append(fresh, m.events...)
	if len(m.events) > maxEvents {
		m.events = m.events[:maxEvents]
	}
}

func isNotable(c Category) bool {
	return c == CategoryRunning || c == CategoryFailed || c == CategoryPassed
}

func computeStats(pipelines map[int64]Pipeline, projects int, now time.Time) MonitorStats {
	s := MonitorStats{TotalProjects: projects, TotalPipelines: len(pipelines), LastPollTime: now}
	for _, p := range pipelines {
		switch p.Category() {
		case CategoryRunning:
			s.Running++
		case CategoryQueued:
			s.Queued++
		case CategoryFailed:
			s.Failed++
		case CategoryPassed:
			s.Passed++
		default:
			s.Other++
		}
	}
	s.Active = s.Running + s.Queued
	return s
}

// Keep only the newest snapshot buffered so slow readers never receive stale data.
func (m *Monitor) broadcast() {
	snap := m.Snapshot()

	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- snap:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}
