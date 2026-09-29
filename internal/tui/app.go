package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"allpipes/internal/gitlab"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var spinnerFrames = []string{"|", "/", "-", "\\"}

type snapshotMsg gitlab.MonitorSnapshot
type tickMsg time.Time

type Model struct {
	monitor     *gitlab.Monitor
	subChan     chan gitlab.MonitorSnapshot
	snapshot    gitlab.MonitorSnapshot
	cursor      int
	offset      int
	filter      string
	filtered    []gitlab.Pipeline
	knownPrev   map[int64]string
	searching   bool
	searchInput textinput.Model
	width       int
	height      int
	spinnerIdx  int
	toastMsg    string
	toastColor  string
	toastExpiry time.Time
}

func NewModel(monitor *gitlab.Monitor) Model {
	ti := textinput.New()
	ti.Placeholder = "Filter by project, branch, sha, author..."
	ti.Prompt = "/ "
	ti.CharLimit = 50

	sub := monitor.Subscribe()
	snap := monitor.GetSnapshot()

	m := Model{
		monitor:     monitor,
		subChan:     sub,
		snapshot:    snap,
		cursor:      0,
		offset:      0,
		filter:      "active",
		knownPrev:   make(map[int64]string),
		searchInput: ti,
		width:       100,
		height:      30,
	}

	for _, p := range snap.Pipelines {
		m.knownPrev[p.ID] = p.Status
	}
	m.applyFilter()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForSnapshot(m.subChan),
		tickEverySecond(),
	)
}

func waitForSnapshot(subChan chan gitlab.MonitorSnapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-subChan
		if !ok {
			return nil
		}
		return snapshotMsg(snap)
	}
}

func tickEverySecond() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case snapshotMsg:
		snap := gitlab.MonitorSnapshot(msg)
		for _, p := range snap.Pipelines {
			prev, exists := m.knownPrev[p.ID]
			if exists && prev != p.Status {
				if p.Status == "failed" {
					m.toastMsg = fmt.Sprintf("FAILED: Pipeline #%d in %s [%s] failed", p.ID, p.ProjectName, p.Ref)
					m.toastColor = "fail"
					m.toastExpiry = time.Now().Add(7 * time.Second)
				} else if p.Status == "success" && prev == "running" {
					m.toastMsg = fmt.Sprintf("SUCCESS: Pipeline #%d in %s [%s] passed", p.ID, p.ProjectName, p.Ref)
					m.toastColor = "success"
					m.toastExpiry = time.Now().Add(7 * time.Second)
				}
			}
			m.knownPrev[p.ID] = p.Status
		}

		m.snapshot = snap
		m.applyFilter()
		return m, waitForSnapshot(m.subChan)

	case tickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		if !m.toastExpiry.IsZero() && time.Now().After(m.toastExpiry) {
			m.toastMsg = ""
		}
		return m, tickEverySecond()

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "enter", "esc":
				m.searching = false
				m.searchInput.Blur()
				return m, nil
			default:
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.applyFilter()
				return m, cmd
			}
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.monitor.Unsubscribe(m.subChan)
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				maxVisible := m.tableHeight()
				if m.cursor >= m.offset+maxVisible {
					m.offset = m.cursor - maxVisible + 1
				}
			}

		case "tab", "f":
			switch m.filter {
			case "active":
				m.filter = "all"
			case "all":
				m.filter = "failed"
			case "failed":
				m.filter = "success"
			case "success":
				m.filter = "active"
			}
			m.cursor = 0
			m.offset = 0
			m.applyFilter()

		case "1":
			m.filter = "active"
			m.cursor = 0
			m.applyFilter()

		case "2":
			m.filter = "all"
			m.cursor = 0
			m.applyFilter()

		case "3":
			m.filter = "failed"
			m.cursor = 0
			m.applyFilter()

		case "4":
			m.filter = "success"
			m.cursor = 0
			m.applyFilter()

		case "/":
			m.searching = true
			m.searchInput.Focus()
			return m, textinput.Blink

		case "r":
			m.monitor.TriggerRefresh()
			m.toastMsg = "Refresh requested..."
			m.toastColor = "info"
			m.toastExpiry = time.Now().Add(2 * time.Second)

		case "enter", "o":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				p := m.filtered[m.cursor]
				if p.WebURL != "" {
					_ = openBrowser(p.WebURL)
				}
			}

		case "p":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				p := m.filtered[m.cursor]
				if p.ProjectWebURL != "" {
					_ = openBrowser(p.ProjectWebURL)
				}
			}
		}
	}

	return m, nil
}

func (m *Model) tableHeight() int {
	h := m.height - 14
	if h < 5 {
		return 5
	}
	return h
}

func (m *Model) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	var res []gitlab.Pipeline

	for _, p := range m.snapshot.Pipelines {
		switch m.filter {
		case "active":
			if !p.IsActive() {
				continue
			}
		case "failed":
			if !p.IsFailed() {
				continue
			}
		case "success":
			if !p.IsSuccess() {
				continue
			}
		}

		if query != "" {
			proj := strings.ToLower(p.ProjectName)
			ref := strings.ToLower(p.Ref)
			sha := strings.ToLower(p.SHA)
			idStr := fmt.Sprintf("%d", p.ID)
			author := ""
			if p.User != nil {
				author = strings.ToLower(p.User.Name + " " + p.User.Username)
			}
			if !strings.Contains(proj, query) && !strings.Contains(ref, query) &&
				!strings.Contains(sha, query) && !strings.Contains(idStr, query) &&
				!strings.Contains(author, query) {
				continue
			}
		}

		res = append(res, p)
	}

	m.filtered = res
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
		m.offset = 0
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var s strings.Builder

	title := HeaderTitleStyle.Render(" ALLPIPES ")
	groupName := m.snapshot.Group.FullPath
	if groupName == "" {
		groupName = m.snapshot.Group.Name
	}
	if groupName == "" {
		groupName = "Loading group..."
	}
	groupInfo := GroupBadgeStyle.Render(fmt.Sprintf(" Group: %s", groupName))
	updated := fmt.Sprintf("Updated: %s", m.snapshot.Stats.LastPollTime.Format("15:04:05"))
	headerRight := lipgloss.NewStyle().Foreground(LightGray).Render(updated)

	leftHeader := lipgloss.JoinHorizontal(lipgloss.Center, title, groupInfo)
	headerGap := m.width - lipgloss.Width(leftHeader) - lipgloss.Width(headerRight) - 2
	if headerGap < 1 {
		headerGap = 1
	}
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, leftHeader, strings.Repeat(" ", headerGap), headerRight) + "\n\n")

	statActive := StatPillBase.Copy().Foreground(ColorRunning).Render(fmt.Sprintf("ACTIVE: %d", m.snapshot.Stats.ActivePipelines))
	statRunning := StatRunningStyle.Render(fmt.Sprintf("RUNNING: %d", m.snapshot.Stats.RunningCount))
	statPending := StatPendingStyle.Render(fmt.Sprintf("PENDING: %d", m.snapshot.Stats.PendingCount))
	statFailed := StatFailedStyle.Render(fmt.Sprintf("FAILED: %d", m.snapshot.Stats.FailedCount))
	statSuccess := StatSuccessStyle.Render(fmt.Sprintf("SUCCESS: %d", m.snapshot.Stats.SuccessCount))
	statProjects := StatProjectsStyle.Render(fmt.Sprintf("PROJECTS: %d", m.snapshot.Stats.TotalProjects))

	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, statActive, statRunning, statPending, statFailed, statSuccess, statProjects) + "\n\n")

	if m.toastMsg != "" {
		toastStyle := ToastFailStyle
		if m.toastColor == "success" {
			toastStyle = ToastSuccessStyle
		} else if m.toastColor == "info" {
			toastStyle = lipgloss.NewStyle().Bold(true).Foreground(TextWhite).Background(lipgloss.Color("#0284c7")).Padding(0, 1)
		}
		s.WriteString(toastStyle.Width(m.width - 2).Render(" "+m.toastMsg) + "\n\n")
	}

	var tabActive, tabAll, tabFailed, tabSuccess string
	if m.filter == "active" {
		tabActive = ActiveTabStyle.Render("[1] Active")
	} else {
		tabActive = InactiveTabStyle.Render("[1] Active")
	}

	if m.filter == "all" {
		tabAll = ActiveTabStyle.Render("[2] All")
	} else {
		tabAll = InactiveTabStyle.Render("[2] All")
	}

	if m.filter == "failed" {
		tabFailed = ActiveTabStyle.Render("[3] Failed")
	} else {
		tabFailed = InactiveTabStyle.Render("[3] Failed")
	}

	if m.filter == "success" {
		tabSuccess = ActiveTabStyle.Render("[4] Success")
	} else {
		tabSuccess = InactiveTabStyle.Render("[4] Success")
	}

	tabs := lipgloss.JoinHorizontal(lipgloss.Top, tabActive, " ", tabAll, " ", tabFailed, " ", tabSuccess)
	searchBar := ""
	if m.searching || m.searchInput.Value() != "" {
		searchBar = m.searchInput.View()
	} else {
		searchBar = lipgloss.NewStyle().Foreground(LightGray).Render("Press [/] to search")
	}

	tabGap := m.width - lipgloss.Width(tabs) - lipgloss.Width(searchBar) - 2
	if tabGap < 2 {
		tabGap = 2
	}
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, tabs, strings.Repeat(" ", tabGap), searchBar) + "\n")

	colStatus := lipgloss.NewStyle().Width(14).Render("STATUS")
	colProject := lipgloss.NewStyle().Width(24).Render("PROJECT")
	colRef := lipgloss.NewStyle().Width(20).Render("BRANCH / REF")
	colPipe := lipgloss.NewStyle().Width(12).Render("PIPELINE")
	colDuration := lipgloss.NewStyle().Width(14).Render("DURATION")
	colUser := lipgloss.NewStyle().Width(18).Render("AUTHOR")

	tableHeader := TableHeaderStyle.Width(m.width - 2).Render(
		fmt.Sprintf(" %-14s %-24s %-20s %-12s %-14s %-18s", colStatus, colProject, colRef, colPipe, colDuration, colUser),
	)
	s.WriteString(tableHeader + "\n")

	if len(m.filtered) == 0 {
		emptyMsg := lipgloss.NewStyle().
			Foreground(LightGray).
			Padding(2, 0).
			Render("  No pipelines found for the current filter.")
		s.WriteString(emptyMsg + "\n")
	} else {
		maxVisible := m.tableHeight()
		end := m.offset + maxVisible
		if end > len(m.filtered) {
			end = len(m.filtered)
		}

		for i := m.offset; i < end; i++ {
			p := m.filtered[i]
			isCursor := i == m.cursor

			statusStr := ""
			switch p.Status {
			case "running":
				spin := spinnerFrames[m.spinnerIdx]
				statusStr = BadgeRunning.Render(fmt.Sprintf("%s RUNNING", spin))
			case "pending", "preparing", "waiting_for_resource":
				statusStr = BadgePending.Render("PENDING")
			case "success":
				statusStr = BadgeSuccess.Render("SUCCESS")
			case "failed":
				statusStr = BadgeFailed.Render("FAILED")
			case "canceled":
				statusStr = BadgeCanceled.Render("CANCELED")
			default:
				statusStr = lipgloss.NewStyle().Foreground(LightGray).Render(strings.ToUpper(p.Status))
			}

			projName := truncateString(p.ProjectName, 22)
			refName := truncateString(p.Ref, 18)
			pipeID := fmt.Sprintf("#%d", p.ID)
			durStr := p.FormattedDuration()

			author := "-"
			if p.User != nil {
				author = truncateString(p.User.Name, 16)
			}

			prefix := "  "
			if isCursor {
				prefix = "> "
			}

			line := fmt.Sprintf("%s%-14s %-24s %-20s %-12s %-14s %-18s",
				prefix,
				statusStr,
				projName,
				refName,
				pipeID,
				durStr,
				author,
			)

			if isCursor {
				s.WriteString(SelectedRowStyle.Width(m.width - 2).Render(line) + "\n")
			} else {
				s.WriteString(RowStyle.Width(m.width - 2).Render(line) + "\n")
			}
		}
	}

	s.WriteString("\n")
	if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
		selected := m.filtered[m.cursor]
		sha := selected.ShortSHA()
		url := selected.WebURL

		details := fmt.Sprintf(
			"Project: %s | Ref: %s | SHA: %s | Created: %s\nURL: %s",
			selected.ProjectPath,
			selected.Ref,
			sha,
			selected.CreatedAt.Format("2006-01-02 15:04:05"),
			url,
		)
		s.WriteString(DetailBoxStyle.Width(m.width - 4).Render(details) + "\n")
	}

	s.WriteString(
		lipgloss.JoinHorizontal(
			lipgloss.Center,
			FooterKeyStyle.Render(" [up/down]"), FooterDescStyle.Render(" navigate  "),
			FooterKeyStyle.Render("[enter/o]"), FooterDescStyle.Render(" open pipeline  "),
			FooterKeyStyle.Render("[p]"), FooterDescStyle.Render(" open project  "),
			FooterKeyStyle.Render("[tab/1-4]"), FooterDescStyle.Render(" filters  "),
			FooterKeyStyle.Render("[r]"), FooterDescStyle.Render(" refresh  "),
			FooterKeyStyle.Render("[/]"), FooterDescStyle.Render(" search  "),
			FooterKeyStyle.Render("[q]"), FooterDescStyle.Render(" quit"),
		) + "\n",
	)

	return s.String()
}

func truncateString(str string, maxLen int) string {
	if len(str) > maxLen {
		return str[:maxLen-1] + "..."
	}
	return str
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}

func Run(ctx context.Context, monitor *gitlab.Monitor) error {
	p := tea.NewProgram(
		NewModel(monitor),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
