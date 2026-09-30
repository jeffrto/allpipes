package tui

import (
	"allpipes/internal/gitlab"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorInk    = lipgloss.AdaptiveColor{Light: "#1b2023", Dark: "#e3e7e8"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#69747a", Dark: "#879297"}
	colorFaint  = lipgloss.AdaptiveColor{Light: "#c5cdd0", Dark: "#343c40"}
	colorSelect = lipgloss.AdaptiveColor{Light: "#e1e8eb", Dark: "#1f292e"}

	colorRunning = lipgloss.AdaptiveColor{Light: "#1766c2", Dark: "#5ea8f2"}
	colorQueued  = lipgloss.AdaptiveColor{Light: "#a3670b", Dark: "#e0aa4c"}
	colorFailed  = lipgloss.AdaptiveColor{Light: "#c3352a", Dark: "#f0705f"}
	colorPassed  = lipgloss.AdaptiveColor{Light: "#1c7c44", Dark: "#56c287"}
	colorOther   = lipgloss.AdaptiveColor{Light: "#7d878c", Dark: "#7a858a"}

	styleInk   = lipgloss.NewStyle().Foreground(colorInk)
	styleBold  = styleInk.Bold(true)
	styleMuted = lipgloss.NewStyle().Foreground(colorMuted)
	styleFaint = lipgloss.NewStyle().Foreground(colorFaint)
	styleLink  = styleMuted.Underline(true)
	styleTab   = styleMuted.Padding(0, 1)
	styleTabOn = styleBold.Background(colorSelect).Padding(0, 1)
)

func categoryColor(c gitlab.Category) lipgloss.AdaptiveColor {
	switch c {
	case gitlab.CategoryRunning:
		return colorRunning
	case gitlab.CategoryQueued:
		return colorQueued
	case gitlab.CategoryFailed:
		return colorFailed
	case gitlab.CategoryPassed:
		return colorPassed
	default:
		return colorOther
	}
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func statusMark(status string, frame int) (glyph, label string) {
	switch status {
	case "running":
		return spinner[frame%len(spinner)], "running"
	case "canceling":
		return spinner[frame%len(spinner)], "canceling"
	case "created", "pending", "preparing":
		return "◌", status
	case "waiting_for_resource":
		return "◌", "waiting"
	case "waiting_for_callback":
		return "◌", "callback"
	case "success":
		return "✓", "passed"
	case "failed":
		return "✕", "failed"
	case "canceled":
		return "⊘", "canceled"
	case "skipped":
		return "»", "skipped"
	case "manual":
		return "▸", "manual"
	case "scheduled":
		return "◷", "scheduled"
	default:
		return "·", status
	}
}

var sourceLabels = map[string]string{
	"merge_request_event":           "MR",
	"parent_pipeline":               "child",
	"pipeline":                      "upstream",
	"external_pull_request_event":   "PR",
	"ondemand_dast_scan":            "dast",
	"security_orchestration_policy": "policy",
}

func sourceLabel(source string) string {
	if l, ok := sourceLabels[source]; ok {
		return l
	}
	return source
}
