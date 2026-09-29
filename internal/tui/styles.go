package tui

import "github.com/charmbracelet/lipgloss"

var (
	GitLabOrange = lipgloss.Color("#fc6d26")
	GitLabRed    = lipgloss.Color("#e24329")
	DarkGray     = lipgloss.Color("#1e293b")
	BorderGray   = lipgloss.Color("#334155")
	LightGray    = lipgloss.Color("#94a3b8")
	TextWhite    = lipgloss.Color("#f8fafc")

	ColorRunning  = lipgloss.Color("#38bdf8")
	ColorPending  = lipgloss.Color("#fbbf24")
	ColorSuccess  = lipgloss.Color("#34d399")
	ColorFailed   = lipgloss.Color("#f87171")
	ColorCanceled = lipgloss.Color("#64748b")

	HeaderTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(GitLabOrange).
				Padding(0, 1)

	GroupBadgeStyle = lipgloss.NewStyle().
			Foreground(GitLabOrange).
			Bold(true)

	StatPillBase = lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Bold(true)

	StatRunningStyle = StatPillBase.Copy().
				Foreground(ColorRunning).
				Background(lipgloss.Color("#0c4a6e"))

	StatPendingStyle = StatPillBase.Copy().
				Foreground(ColorPending).
				Background(lipgloss.Color("#451a03"))

	StatFailedStyle = StatPillBase.Copy().
				Foreground(ColorFailed).
				Background(lipgloss.Color("#450a0a"))

	StatSuccessStyle = StatPillBase.Copy().
				Foreground(ColorSuccess).
				Background(lipgloss.Color("#022c22"))

	StatProjectsStyle = StatPillBase.Copy().
				Foreground(LightGray).
				Background(lipgloss.Color("#1e293b"))

	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextWhite).
			Background(GitLabOrange).
			Padding(0, 1)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(LightGray).
				Padding(0, 1)

	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(LightGray).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(BorderGray)

	RowStyle = lipgloss.NewStyle().
			Padding(0, 0)

	SelectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e293b")).
				Bold(true)

	BadgeRunning = lipgloss.NewStyle().
			Foreground(ColorRunning).
			Bold(true)

	BadgePending = lipgloss.NewStyle().
			Foreground(ColorPending).
			Bold(true)

	BadgeSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	BadgeFailed = lipgloss.NewStyle().
			Foreground(ColorFailed).
			Bold(true)

	BadgeCanceled = lipgloss.NewStyle().
			Foreground(ColorCanceled)

	ToastFailStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#b91c1c")).
			Padding(0, 1)

	ToastSuccessStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#047857")).
			Padding(0, 1)

	FooterKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(GitLabOrange)

	FooterDescStyle = lipgloss.NewStyle().
			Foreground(LightGray)

	DetailBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderGray).
			Padding(0, 1)
)
