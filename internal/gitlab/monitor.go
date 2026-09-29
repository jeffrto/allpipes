package gitlab

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type MonitorOptions struct {
	GroupIDOrPath       string
	IncludeSubgroups    bool
	PollInterval        time.Duration
	PipelinesPerProject int
	Concurrency         int
}

type Monitor struct {
	client  *Client
	options MonitorOptions

	mu        sync.RWMutex
	group     Group
	projects  []Project
	pipelines map[int64]Pipeline
	events    []PipelineEvent
	stats     MonitorStats
	lastError error

	subscribers   map[chan MonitorSnapshot]struct{}
	subscribersMu sync.RWMutex

	refreshChan chan struct{}
}

func NewMonitor(client *Client, opts MonitorOptions) *Monitor {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 10 * time.Second
	}
	if opts.PipelinesPerProject <= 0 {
		opts.PipelinesPerProject = 3
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}

	return &Monitor{
		client:      client,
		options:     opts,
		pipelines:   make(map[int64]Pipeline),
		events:      make([]PipelineEvent, 0, 100),
		subscribers: make(map[chan MonitorSnapshot]struct{}),
		refreshChan: make(chan struct{}, 1),
	}
}

func (m *Monitor) Subscribe() chan MonitorSnapshot {
	m.subscribersMu.Lock()
	defer m.subscribersMu.Unlock()

	ch := make(chan MonitorSnapshot, 10)
	m.subscribers[ch] = struct{}{}
	return ch
}

func (m *Monitor) Unsubscribe(ch chan MonitorSnapshot) {
	m.subscribersMu.Lock()
	defer m.subscribersMu.Unlock()

	if _, ok := m.subscribers[ch]; ok {
		delete(m.subscribers, ch)
		close(ch)
	}
}

func (m *Monitor) TriggerRefresh() {
	select {
	case m.refreshChan <- struct{}{}:
	default:
	}
}

func (m *Monitor) GetSnapshot() MonitorSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pipeList := make([]Pipeline, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		pipeList = append(pipeList, p)
	}

	sort.Slice(pipeList, func(i, j int) bool {
		iActive := pipeList[i].IsActive()
		jActive := pipeList[j].IsActive()
		if iActive != jActive {
			return iActive
		}
		return pipeList[i].UpdatedAt.After(pipeList[j].UpdatedAt)
	})

	eventsCopy := make([]PipelineEvent, len(m.events))
	copy(eventsCopy, m.events)

	return MonitorSnapshot{
		Group:     m.group,
		Stats:     m.stats,
		Pipelines: pipeList,
		Events:    eventsCopy,
		Timestamp: time.Now(),
	}
}

func (m *Monitor) GetLastError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastError
}

func (m *Monitor) Start(ctx context.Context) error {
	group, err := m.client.GetGroup(ctx, m.options.GroupIDOrPath)
	if err != nil {
		return fmt.Errorf("failed to fetch gitlab group '%s': %w", m.options.GroupIDOrPath, err)
	}

	m.mu.Lock()
	m.group = *group
	m.mu.Unlock()

	if err := m.refreshProjects(ctx); err != nil {
		return fmt.Errorf("failed to fetch projects for group '%s': %w", m.options.GroupIDOrPath, err)
	}

	m.pollPipelines(ctx)
	m.broadcastSnapshot()

	ticker := time.NewTicker(m.options.PollInterval)
	defer ticker.Stop()

	projectRefreshTicker := time.NewTicker(5 * time.Minute)
	defer projectRefreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-projectRefreshTicker.C:
			_ = m.refreshProjects(ctx)

		case <-ticker.C:
			m.pollPipelines(ctx)
			m.broadcastSnapshot()

		case <-m.refreshChan:
			m.pollPipelines(ctx)
			m.broadcastSnapshot()
		}
	}
}

func (m *Monitor) refreshProjects(ctx context.Context) error {
	projects, err := m.client.GetGroupProjects(ctx, m.options.GroupIDOrPath, m.options.IncludeSubgroups)
	if err != nil {
		m.mu.Lock()
		m.lastError = err
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	m.projects = projects
	m.stats.TotalProjects = len(projects)
	m.lastError = nil
	m.mu.Unlock()
	return nil
}

func (m *Monitor) pollPipelines(ctx context.Context) {
	m.mu.RLock()
	projects := make([]Project, len(m.projects))
	copy(projects, m.projects)
	m.mu.RUnlock()

	if len(projects) == 0 {
		return
	}

	type projectResult struct {
		project   Project
		pipelines []Pipeline
		err       error
	}

	resChan := make(chan projectResult, len(projects))
	workChan := make(chan Project, len(projects))

	numWorkers := m.options.Concurrency
	if numWorkers > len(projects) {
		numWorkers = len(projects)
	}

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for prj := range workChan {
				pipes, err := m.client.GetProjectPipelines(ctx, prj.ID, m.options.PipelinesPerProject)
				resChan <- projectResult{
					project:   prj,
					pipelines: pipes,
					err:       err,
				}
			}
		}()
	}

	for _, p := range projects {
		workChan <- p
	}
	close(workChan)

	wg.Wait()
	close(resChan)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	newEvents := make([]PipelineEvent, 0)

	for res := range resChan {
		if res.err != nil {
			m.lastError = res.err
			continue
		}

		for _, p := range res.pipelines {
			p.ProjectName = res.project.Name
			p.ProjectPath = res.project.PathWithNamespace
			p.ProjectWebURL = res.project.WebURL

			existing, exists := m.pipelines[p.ID]
			if !exists {
				if p.IsRunning() {
					event := PipelineEvent{
						ID:             fmt.Sprintf("%d-%s-%d", p.ID, p.Status, now.UnixNano()),
						Type:           EventPipelineStarted,
						Pipeline:       p,
						PreviousStatus: "none",
						NewStatus:      p.Status,
						Timestamp:      now,
						Message:        fmt.Sprintf("Pipeline #%d started in %s on [%s]", p.ID, p.ProjectName, p.Ref),
					}
					newEvents = append(newEvents, event)
				}
			} else {
				if existing.Status != p.Status {
					var eventType PipelineEventType = EventPipelineChanged
					var msg string

					switch p.Status {
					case "success":
						eventType = EventPipelineFinished
						dur := p.FormattedDuration()
						msg = fmt.Sprintf("Pipeline #%d succeeded in %s on [%s] (duration: %s)", p.ID, p.ProjectName, p.Ref, dur)
					case "failed":
						eventType = EventPipelineFinished
						msg = fmt.Sprintf("Pipeline #%d failed in %s on [%s]", p.ID, p.ProjectName, p.Ref)
					case "canceled":
						eventType = EventPipelineFinished
						msg = fmt.Sprintf("Pipeline #%d was canceled in %s on [%s]", p.ID, p.ProjectName, p.Ref)
					case "running":
						eventType = EventPipelineStarted
						msg = fmt.Sprintf("Pipeline #%d is now running in %s on [%s]", p.ID, p.ProjectName, p.Ref)
					default:
						msg = fmt.Sprintf("Pipeline #%d in %s changed status from %s to %s", p.ID, p.ProjectName, existing.Status, p.Status)
					}

					event := PipelineEvent{
						ID:             fmt.Sprintf("%d-%s-%d", p.ID, p.Status, now.UnixNano()),
						Type:           eventType,
						Pipeline:       p,
						PreviousStatus: existing.Status,
						NewStatus:      p.Status,
						Timestamp:      now,
						Message:        msg,
					}
					newEvents = append(newEvents, event)
				}
			}

			m.pipelines[p.ID] = p
		}
	}

	if len(newEvents) > 0 {
		m.events = append(newEvents, m.events...)
		if len(m.events) > 100 {
			m.events = m.events[:100]
		}
	}

	var running, pending, failed, success, canceled, active int
	for _, p := range m.pipelines {
		switch p.Status {
		case "running":
			running++
			active++
		case "pending", "preparing", "waiting_for_resource":
			pending++
			active++
		case "failed":
			failed++
		case "success":
			success++
		case "canceled":
			canceled++
		}
	}

	m.stats.TotalPipelines = len(m.pipelines)
	m.stats.ActivePipelines = active
	m.stats.RunningCount = running
	m.stats.PendingCount = pending
	m.stats.FailedCount = failed
	m.stats.SuccessCount = success
	m.stats.CanceledCount = canceled
	m.stats.LastPollTime = now
}

func (m *Monitor) broadcastSnapshot() {
	snap := m.GetSnapshot()

	m.subscribersMu.RLock()
	defer m.subscribersMu.RUnlock()

	for ch := range m.subscribers {
		select {
		case ch <- snap:
		default:
		}
	}
}
