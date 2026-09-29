package gitlab

import (
	"fmt"
	"time"
)

type Group struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	FullPath    string `json:"full_path"`
	Description string `json:"description"`
	WebURL      string `json:"web_url"`
	AvatarURL   string `json:"avatar_url"`
}

type Project struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	NameWithNamespace string `json:"name_with_namespace"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
	AvatarURL         string `json:"avatar_url"`
	Archived          bool   `json:"archived"`
	EmptyRepo         bool   `json:"empty_repo"`
}

type User struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
}

type Pipeline struct {
	ID            int64      `json:"id"`
	IID           int64      `json:"iid"`
	ProjectID     int64      `json:"project_id"`
	ProjectName   string     `json:"project_name"`
	ProjectPath   string     `json:"project_path"`
	ProjectWebURL string     `json:"project_web_url"`
	Status        string     `json:"status"`
	Source        string     `json:"source"`
	Ref           string     `json:"ref"`
	SHA           string     `json:"sha"`
	Name          string     `json:"name"`
	WebURL        string     `json:"web_url"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	Duration      *int64     `json:"duration,omitempty"`
	User          *User      `json:"user,omitempty"`
	CommitTitle   string     `json:"commit_title,omitempty"`
}

func (p *Pipeline) IsActive() bool {
	switch p.Status {
	case "running", "pending", "preparing", "waiting_for_resource":
		return true
	default:
		return false
	}
}

func (p *Pipeline) IsRunning() bool {
	return p.Status == "running"
}

func (p *Pipeline) IsFailed() bool {
	return p.Status == "failed"
}

func (p *Pipeline) IsSuccess() bool {
	return p.Status == "success"
}

func (p *Pipeline) ShortSHA() string {
	if len(p.SHA) > 8 {
		return p.SHA[:8]
	}
	return p.SHA
}

func (p *Pipeline) ElapsedDuration() time.Duration {
	if p.Duration != nil && *p.Duration > 0 {
		return time.Duration(*p.Duration) * time.Second
	}
	if p.StartedAt != nil {
		if p.FinishedAt != nil {
			return p.FinishedAt.Sub(*p.StartedAt)
		}
		return time.Since(*p.StartedAt)
	}
	if p.IsRunning() && !p.CreatedAt.IsZero() {
		return time.Since(p.CreatedAt)
	}
	return 0
}

func (p *Pipeline) FormattedDuration() string {
	d := p.ElapsedDuration()
	if d <= 0 {
		if p.Status == "pending" {
			return "queued"
		}
		return "-"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02dh %02dm %02ds", h, m, s)
	}
	return fmt.Sprintf("%02dm %02ds", m, s)
}

type PipelineEventType string

const (
	EventPipelineStarted  PipelineEventType = "started"
	EventPipelineFinished PipelineEventType = "finished"
	EventPipelineChanged  PipelineEventType = "changed"
)

type PipelineEvent struct {
	ID             string            `json:"id"`
	Type           PipelineEventType `json:"type"`
	Pipeline       Pipeline          `json:"pipeline"`
	PreviousStatus string            `json:"previous_status"`
	NewStatus      string            `json:"new_status"`
	Timestamp      time.Time         `json:"timestamp"`
	Message        string            `json:"message"`
}

type MonitorStats struct {
	TotalProjects   int       `json:"total_projects"`
	TotalPipelines  int       `json:"total_pipelines"`
	ActivePipelines int       `json:"active_pipelines"`
	RunningCount    int       `json:"running_count"`
	PendingCount    int       `json:"pending_count"`
	FailedCount     int       `json:"failed_count"`
	SuccessCount    int       `json:"success_count"`
	CanceledCount   int       `json:"canceled_count"`
	LastPollTime    time.Time `json:"last_poll_time"`
}

type MonitorSnapshot struct {
	Group     Group           `json:"group"`
	Stats     MonitorStats    `json:"stats"`
	Pipelines []Pipeline      `json:"pipelines"`
	Events    []PipelineEvent `json:"events"`
	Timestamp time.Time       `json:"timestamp"`
}
