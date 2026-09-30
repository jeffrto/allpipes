package gitlab

import (
	"fmt"
	"time"
)

type Group struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullPath string `json:"full_path"`
	WebURL   string `json:"web_url"`
}

type Project struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
}

type User struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
}

type Pipeline struct {
	ID             int64      `json:"id"`
	IID            int64      `json:"iid"`
	ProjectID      int64      `json:"project_id"`
	ProjectName    string     `json:"project_name"`
	ProjectPath    string     `json:"project_path"`
	ProjectWebURL  string     `json:"project_web_url"`
	Status         string     `json:"status"`
	Source         string     `json:"source"`
	Ref            string     `json:"ref"`
	SHA            string     `json:"sha"`
	Name           string     `json:"name"`
	WebURL         string     `json:"web_url"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Duration       *float64   `json:"duration,omitempty"`
	QueuedDuration *float64   `json:"queued_duration,omitempty"`
	User           *User      `json:"user,omitempty"`

	detailed bool
}

type Category string

const (
	CategoryRunning Category = "running"
	CategoryQueued  Category = "queued"
	CategoryFailed  Category = "failed"
	CategoryPassed  Category = "passed"
	CategoryOther   Category = "other"
)

func (p Pipeline) Category() Category {
	switch p.Status {
	case "running", "canceling":
		return CategoryRunning
	case "created", "waiting_for_resource", "preparing", "waiting_for_callback", "pending":
		return CategoryQueued
	case "failed":
		return CategoryFailed
	case "success":
		return CategoryPassed
	default:
		return CategoryOther
	}
}

func (p Pipeline) IsActive() bool {
	c := p.Category()
	return c == CategoryRunning || c == CategoryQueued
}

func (p Pipeline) ShortSHA() string {
	if len(p.SHA) > 8 {
		return p.SHA[:8]
	}
	return p.SHA
}

func (p Pipeline) Elapsed(now time.Time) time.Duration {
	if p.Duration != nil && *p.Duration > 0 && !p.IsActive() {
		return time.Duration(*p.Duration * float64(time.Second))
	}
	if p.StartedAt != nil {
		end := now
		if p.FinishedAt != nil {
			end = *p.FinishedAt
		}
		if d := end.Sub(*p.StartedAt); d > 0 {
			return d
		}
	}
	return 0
}

func (p Pipeline) LastActivity() time.Time {
	if p.FinishedAt != nil {
		return *p.FinishedAt
	}
	return p.UpdatedAt
}

func FormatDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

type PipelineEvent struct {
	Seq            int64     `json:"seq"`
	Pipeline       Pipeline  `json:"pipeline"`
	PreviousStatus string    `json:"previous_status"`
	NewStatus      string    `json:"new_status"`
	Timestamp      time.Time `json:"timestamp"`
}

type MonitorStats struct {
	TotalProjects  int       `json:"total_projects"`
	TotalPipelines int       `json:"total_pipelines"`
	Active         int       `json:"active"`
	Running        int       `json:"running"`
	Queued         int       `json:"queued"`
	Failed         int       `json:"failed"`
	Passed         int       `json:"passed"`
	Other          int       `json:"other"`
	LastPollTime   time.Time `json:"last_poll_time"`
}

type MonitorSnapshot struct {
	Group               Group           `json:"group"`
	Stats               MonitorStats    `json:"stats"`
	Pipelines           []Pipeline      `json:"pipelines"`
	Events              []PipelineEvent `json:"events"`
	Ready               bool            `json:"ready"`
	Error               string          `json:"error,omitempty"`
	PollInterval        float64         `json:"poll_interval"`
	PipelinesPerProject int             `json:"pipelines_per_project"`
	Timestamp           time.Time       `json:"timestamp"`
}
