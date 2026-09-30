package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"allpipes/internal/gitlab"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type view struct {
	name  string
	match func(gitlab.Pipeline) bool
	count func(gitlab.MonitorStats) int
	empty string
}

func inCategory(c gitlab.Category) func(gitlab.Pipeline) bool {
	return func(p gitlab.Pipeline) bool { return p.Category() == c }
}

var views = []view{
	{"All", func(gitlab.Pipeline) bool { return true }, func(s gitlab.MonitorStats) int { return s.TotalPipelines }, "No pipelines in this group yet."},
	{"Active", gitlab.Pipeline.IsActive, func(s gitlab.MonitorStats) int { return s.Active }, "Nothing is running or queued right now."},
	{"Failed", inCategory(gitlab.CategoryFailed), func(s gitlab.MonitorStats) int { return s.Failed }, "No failed pipelines."},
	{"Passed", inCategory(gitlab.CategoryPassed), func(s gitlab.MonitorStats) int { return s.Passed }, "No passed pipelines."},
}

type snapshotMsg gitlab.MonitorSnapshot
type tickMsg time.Time

type notice struct {
	text  string
	color lipgloss.TerminalColor
	until time.Time
}

type Model struct {
	monitor   *gitlab.Monitor
	updates   chan gitlab.MonitorSnapshot
	snap      gitlab.MonitorSnapshot
	rows      []gitlab.Pipeline
	view      int
	cursor    int
	offset    int
	selected  int64
	search    textinput.Model
	searching bool
	width     int
	height    int
	frame     int
	now       time.Time
	lastSeq   int64
	notice    notice
}

func newModel(monitor *gitlab.Monitor) Model {
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "project, branch, SHA, author"
	search.CharLimit = 64
	search.Width = 30
	search.PromptStyle = styleInk
	search.TextStyle = styleInk
	search.PlaceholderStyle = styleMuted

	updates := monitor.Subscribe()
	snap := monitor.Snapshot()
	m := Model{
		monitor: monitor,
		updates: updates,
		snap:    snap,
		search:  search,
		width:   100,
		height:  30,
		now:     time.Now(),
		lastSeq: latestSeq(snap.Events),
	}
	m.refilter()
	return m
}

func Run(ctx context.Context, monitor *gitlab.Monitor) error {
	// Detect the terminal background before Bubble Tea takes over stdin,
	// otherwise the adaptive colors cannot query it.
	lipgloss.HasDarkBackground()

	p := tea.NewProgram(newModel(monitor), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitFor(m.updates), tick())
}

func waitFor(updates chan gitlab.MonitorSnapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-updates
		if !ok {
			return nil
		}
		return snapshotMsg(snap)
	}
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
	case snapshotMsg:
		m.snap = gitlab.MonitorSnapshot(msg)
		m.announce()
		m.refilter()
		return m, waitFor(m.updates)
	case tickMsg:
		m.now = time.Time(msg)
		m.frame++
		return m, tick()
	case tea.KeyMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, m.quit()
	case tea.KeyEsc:
		m.search.SetValue("")
		m.searching = false
		m.search.Blur()
	case tea.KeyEnter:
		m.searching = false
		m.search.Blur()
	case tea.KeyUp:
		m.move(-1)
		return m, nil
	case tea.KeyDown:
		m.move(1)
		return m, nil
	default:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		m.refilter()
		return m, cmd
	}
	m.refilter()
	return m, nil
}

func (m Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key := msg.String(); key {
	case "ctrl+c", "q":
		return m, m.quit()
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-m.layout().rows)
	case "pgdown", "ctrl+d":
		m.move(m.layout().rows)
	case "home", "g":
		m.move(-len(m.rows))
	case "end", "G":
		m.move(len(m.rows))
	case "tab", "right", "l":
		m.setView((m.view + 1) % len(views))
	case "shift+tab", "left", "h":
		m.setView((m.view + len(views) - 1) % len(views))
	case "1", "2", "3", "4":
		m.setView(int(key[0] - '1'))
	case "/":
		m.searching = true
		return m, m.search.Focus()
	case "esc":
		m.search.SetValue("")
		m.refilter()
	case "r":
		m.monitor.TriggerRefresh()
		m.flash("Refreshing now", colorMuted, 2*time.Second)
	case "enter", "o":
		if p, ok := m.current(); ok {
			m.open(p.WebURL)
		}
	case "p":
		if p, ok := m.current(); ok {
			m.open(p.ProjectWebURL)
		}
	}
	return m, nil
}

func (m *Model) quit() tea.Cmd {
	m.monitor.Unsubscribe(m.updates)
	return tea.Quit
}

func (m *Model) open(url string) {
	if url == "" {
		return
	}
	if err := openBrowser(url); err != nil {
		m.flash("Could not open a browser: "+err.Error(), colorFailed, 5*time.Second)
		return
	}
	m.flash("Opened "+url, colorMuted, 2*time.Second)
}

func (m *Model) flash(text string, color lipgloss.TerminalColor, d time.Duration) {
	m.notice = notice{text: text, color: color, until: time.Now().Add(d)}
}

func (m *Model) announce() {
	var failed, passed []gitlab.Pipeline
	for _, e := range m.snap.Events {
		if e.Seq <= m.lastSeq {
			break
		}
		switch e.NewStatus {
		case "failed":
			failed = append(failed, e.Pipeline)
		case "success":
			passed = append(passed, e.Pipeline)
		}
	}
	m.lastSeq = max(m.lastSeq, latestSeq(m.snap.Events))

	switch {
	case len(failed) == 1:
		p := failed[0]
		m.flash(fmt.Sprintf("✕ %s failed on %s  #%d", m.projectLabel(p), refLabel(p.Ref), p.ID), colorFailed, 10*time.Second)
	case len(failed) > 1:
		m.flash(fmt.Sprintf("✕ %d pipelines failed, latest %s on %s", len(failed), m.projectLabel(failed[0]), refLabel(failed[0].Ref)), colorFailed, 10*time.Second)
	case len(passed) == 1:
		p := passed[0]
		m.flash(fmt.Sprintf("✓ %s passed on %s  #%d", m.projectLabel(p), refLabel(p.Ref), p.ID), colorPassed, 6*time.Second)
	case len(passed) > 1:
		m.flash(fmt.Sprintf("✓ %d pipelines passed", len(passed)), colorPassed, 6*time.Second)
	}
}

func latestSeq(events []gitlab.PipelineEvent) int64 {
	if len(events) == 0 {
		return 0
	}
	return events[0].Seq
}

func (m *Model) setView(v int) {
	m.view = v
	m.cursor, m.offset, m.selected = 0, 0, 0
	m.refilter()
}

func (m *Model) refilter() {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	match := views[m.view].match
	rows := make([]gitlab.Pipeline, 0, len(m.snap.Pipelines))
	for _, p := range m.snap.Pipelines {
		if match(p) && matchesQuery(p, query) {
			rows = append(rows, p)
		}
	}
	m.rows = rows

	idx := -1
	for i, p := range rows {
		if p.ID == m.selected {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = min(m.cursor, len(rows)-1)
	}
	m.cursor = max(idx, 0)
	if len(rows) > 0 {
		m.selected = rows[m.cursor].ID
	}
	m.clampScroll()
}

func matchesQuery(p gitlab.Pipeline, query string) bool {
	if query == "" {
		return true
	}
	fields := []string{p.ProjectPath, p.Ref, p.SHA, strconv.FormatInt(p.ID, 10), p.Status}
	if p.User != nil {
		fields = append(fields, p.User.Name, p.User.Username)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = max(0, min(m.cursor+delta, len(m.rows)-1))
	m.selected = m.rows[m.cursor].ID
	m.clampScroll()
}

func (m *Model) clampScroll() {
	visible := m.layout().rows
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	m.offset = max(0, min(m.offset, len(m.rows)-visible))
}

func (m Model) current() (gitlab.Pipeline, bool) {
	if m.cursor < len(m.rows) {
		return m.rows[m.cursor], true
	}
	return gitlab.Pipeline{}, false
}

type layout struct {
	compact bool
	rows    int
}

func (m Model) layout() layout {
	compact := m.height < 24
	chrome := 13
	if compact {
		chrome = 6
	}
	if m.snap.Error != "" {
		chrome++
	}
	return layout{compact: compact, rows: max(m.height-chrome, 1)}
}

func (m Model) View() string {
	if m.width < 40 || m.height < 10 {
		return "allpipes needs a terminal of at least 40×10."
	}
	w := m.width - 2
	lay := m.layout()
	cols := columnsFor(w)

	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }

	add(m.viewHeader(w))
	if m.snap.Error != "" {
		msg := strings.ReplaceAll(m.snap.Error, "\n", " ")
		add(lipgloss.NewStyle().Foreground(colorFailed).Render(fit("▲ "+msg, w, false)))
	}
	if !lay.compact {
		add("")
	}
	add(m.viewFlow(w), m.viewCounts(w))
	if !lay.compact {
		add("")
	}
	add(m.viewTabs(w), viewHead(cols))
	if !lay.compact {
		add(styleFaint.Render(strings.Repeat("─", w)))
	}
	add(m.viewRows(cols, w, lay.rows)...)
	if !lay.compact {
		add(m.viewScrollRule(w, lay.rows))
		add(m.viewDetail(w)...)
	}
	add(m.viewFooter(w))

	for i, l := range lines {
		lines[i] = " " + l
	}
	return strings.Join(lines, "\n")
}

func (m Model) viewHeader(w int) string {
	left := styleBold.Render("allpipes") + "  " + styleMuted.Render(m.snap.Group.FullPath)

	var right string
	switch {
	case !m.snap.Ready:
		right = styleMuted.Render(spinner[m.frame%len(spinner)] + " loading")
	default:
		dot := colorPassed
		if m.snap.Error != "" {
			dot = colorFailed
		}
		polled := "polled " + agoShort(m.snap.Stats.LastPollTime, m.now)
		interval := "every " + time.Duration(m.snap.PollInterval*float64(time.Second)).String()
		right = lipgloss.NewStyle().Foreground(dot).Render("●") + " " + styleMuted.Render(polled+" · "+interval)
	}
	return spread(left, right, w)
}

func (m Model) viewFlow(w int) string {
	s := m.snap.Stats
	if s.TotalPipelines == 0 {
		return styleFaint.Render(strings.Repeat("━", w))
	}
	counts := []int{s.Running, s.Queued, s.Failed, s.Passed, s.Other}
	colors := []lipgloss.TerminalColor{colorRunning, colorQueued, colorFailed, colorPassed, colorOther}
	var b strings.Builder
	for i, n := range apportion(counts, w) {
		if n > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Render(strings.Repeat("━", n)))
		}
	}
	return b.String()
}

func apportion(counts []int, width int) []int {
	out := make([]int, len(counts))
	total, nonZero := 0, 0
	for _, c := range counts {
		total += c
		if c > 0 {
			nonZero++
		}
	}
	if total == 0 {
		return out
	}
	spare := width - nonZero
	remainders := make([]float64, len(counts))
	used := 0
	for i, c := range counts {
		if c == 0 {
			remainders[i] = -1
			continue
		}
		exact := float64(c) / float64(total) * float64(spare)
		out[i] = 1 + int(exact)
		used += int(exact)
		remainders[i] = exact - float64(int(exact))
	}
	for ; used < spare; used++ {
		best := 0
		for i := range remainders {
			if remainders[i] > remainders[best] {
				best = i
			}
		}
		out[best]++
		remainders[best] = -1
	}
	return out
}

func (m Model) viewCounts(w int) string {
	s := m.snap.Stats
	items := []struct {
		n     int
		label string
		color lipgloss.TerminalColor
	}{
		{s.Running, "running", colorRunning},
		{s.Queued, "queued", colorQueued},
		{s.Failed, "failed", colorFailed},
		{s.Passed, "passed", colorPassed},
		{s.Other, "other", colorOther},
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		num := styleMuted.Render(strconv.Itoa(it.n))
		if it.n > 0 {
			num = lipgloss.NewStyle().Foreground(it.color).Bold(true).Render(strconv.Itoa(it.n))
		}
		parts = append(parts, num+" "+styleMuted.Render(it.label))
	}
	right := styleMuted.Render(fmt.Sprintf("%d projects · last %d each", s.TotalProjects, m.snap.PipelinesPerProject))
	return spread(strings.Join(parts, "   "), right, w)
}

func (m Model) viewTabs(w int) string {
	tabs := make([]string, 0, len(views))
	for i, v := range views {
		label := fmt.Sprintf("%s %d", v.name, v.count(m.snap.Stats))
		if i == m.view {
			tabs = append(tabs, styleTabOn.Render(label))
		} else {
			tabs = append(tabs, styleTab.Render(label))
		}
	}

	var search string
	switch {
	case m.searching:
		search = m.search.View()
	case m.search.Value() != "":
		search = styleInk.Render("/ "+m.search.Value()) + styleMuted.Render("  esc clears")
	default:
		search = styleMuted.Render("/ filter")
	}
	return spread(strings.Join(tabs, " "), search, w)
}

type column struct {
	key   string
	title string
	width int
	right bool
}

const markerWidth = 2

func columnsFor(w int) []column {
	cols := []column{
		{key: "status", title: "status", width: 11},
		{key: "project", title: "project"},
		{key: "ref", title: "ref"},
		{key: "pipeline", title: "pipeline", width: 11},
		{key: "source", title: "source", width: 8},
		{key: "author", title: "author", width: 16},
		{key: "duration", title: "duration", width: 8, right: true},
		{key: "updated", title: "updated", width: 8, right: true},
	}
	const minFlex = 34
	dropOrder := []string{"source", "author", "pipeline", "updated", "duration"}

	fixedWidth := func() int {
		n := markerWidth + 2*(len(cols)-1)
		for _, c := range cols {
			n += c.width
		}
		return n
	}
	for _, key := range dropOrder {
		if w-fixedWidth() >= minFlex {
			break
		}
		for i, c := range cols {
			if c.key == key {
				cols = append(cols[:i], cols[i+1:]...)
				break
			}
		}
	}

	flex := max(w-fixedWidth(), 12)
	for i := range cols {
		switch cols[i].key {
		case "project":
			cols[i].width = flex * 55 / 100
		case "ref":
			cols[i].width = flex - flex*55/100
		}
	}
	return cols
}

func viewHead(cols []column) string {
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = fit(c.title, c.width, c.right)
	}
	return styleMuted.Render(strings.Repeat(" ", markerWidth) + strings.Join(cells, "  "))
}

func (m Model) viewRows(cols []column, w, n int) []string {
	lines := make([]string, 0, n)
	switch {
	case !m.snap.Ready:
		lines = append(lines, "", "  "+styleMuted.Render(fmt.Sprintf("%s Loading pipelines from %s", spinner[m.frame%len(spinner)], m.snap.Group.FullPath)))
	case len(m.rows) == 0:
		msg := views[m.view].empty
		if q := strings.TrimSpace(m.search.Value()); q != "" {
			msg = fmt.Sprintf("No pipelines match %q.", q)
		} else if m.snap.Stats.TotalProjects == 0 {
			msg = "This group has no projects."
		}
		lines = append(lines, "", "  "+styleMuted.Render(msg))
	default:
		for i := m.offset; i < min(m.offset+n, len(m.rows)); i++ {
			lines = append(lines, m.viewRow(m.rows[i], cols, w, i == m.cursor))
		}
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}

func (m Model) viewRow(p gitlab.Pipeline, cols []column, w int, selected bool) string {
	paint := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(colorSelect)
		}
		return s
	}
	status := lipgloss.NewStyle().Foreground(categoryColor(p.Category()))
	text := styleInk
	if selected {
		text = styleBold
	}

	cells := make([]string, len(cols))
	for i, c := range cols {
		var value string
		style := styleMuted
		switch c.key {
		case "status":
			glyph, label := statusMark(p.Status, m.frame)
			value, style = glyph+" "+label, status
		case "project":
			value, style = m.projectLabel(p), text
		case "ref":
			value, style = refLabel(p.Ref), styleInk
		case "pipeline":
			value = "#" + strconv.FormatInt(p.ID, 10)
		case "source":
			value = sourceLabel(p.Source)
		case "author":
			if p.User != nil {
				value = p.User.Name
			}
		case "duration":
			value = durationText(p, m.now)
			if p.Category() == gitlab.CategoryRunning {
				style = status
			}
		case "updated":
			value = ago(p.LastActivity(), m.now)
		}
		cells[i] = paint(style).Render(fit(value, c.width, c.right))
	}

	marker := "  "
	if selected {
		marker = "▌ "
	}
	row := paint(styleInk).Render(marker) + strings.Join(cells, paint(styleInk).Render("  "))
	if pad := w - lipgloss.Width(row); pad > 0 {
		row += paint(styleInk).Render(strings.Repeat(" ", pad))
	}
	return row
}

func (m Model) viewScrollRule(w, visible int) string {
	if len(m.rows) <= visible {
		return styleFaint.Render(strings.Repeat("─", w))
	}
	label := fmt.Sprintf(" %d–%d of %d ", m.offset+1, min(m.offset+visible, len(m.rows)), len(m.rows))
	return styleFaint.Render(strings.Repeat("─", w-ansi.StringWidth(label)-2)) + styleMuted.Render(label) + styleFaint.Render("──")
}

func (m Model) viewDetail(w int) []string {
	p, ok := m.current()
	if !ok {
		return []string{"", "", ""}
	}

	title := styleBold.Render(p.ProjectPath) + styleMuted.Render("  on  ") + styleInk.Render(refLabel(p.Ref)) + styleMuted.Render("  @ "+p.ShortSHA())

	facts := []string{"#" + strconv.FormatInt(p.ID, 10)}
	if p.Source != "" {
		facts = append(facts, sourceLabel(p.Source))
	}
	if p.User != nil {
		facts = append(facts, "by "+p.User.Name)
	}
	facts = append(facts, "created "+m.clock(p.CreatedAt))
	if p.StartedAt != nil {
		facts = append(facts, "started "+m.clock(*p.StartedAt))
	}
	if p.FinishedAt != nil {
		facts = append(facts, "finished "+m.clock(*p.FinishedAt))
	}
	if d := p.Elapsed(m.now); d > 0 {
		facts = append(facts, "ran "+gitlab.FormatDuration(d))
	}
	if p.QueuedDuration != nil && *p.QueuedDuration >= 1 {
		facts = append(facts, "queued "+gitlab.FormatDuration(time.Duration(*p.QueuedDuration*float64(time.Second))))
	}

	return []string{
		ansi.Truncate(title, w, "…"),
		styleMuted.Render(fit(strings.Join(facts, " · "), w, false)),
		styleLink.Render(fit(p.WebURL, w, false)),
	}
}

func (m Model) viewFooter(w int) string {
	if m.notice.text != "" && m.now.Before(m.notice.until) {
		return lipgloss.NewStyle().Foreground(m.notice.color).Bold(true).Render(fit(m.notice.text, w, false))
	}
	keys := [][2]string{{"↑↓", "move"}, {"enter", "open"}, {"p", "project"}, {"tab", "view"}, {"/", "filter"}, {"r", "refresh"}, {"q", "quit"}}
	if m.searching {
		keys = [][2]string{{"enter", "keep filter"}, {"esc", "clear"}, {"↑↓", "move"}}
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = styleInk.Render(k[0]) + " " + styleMuted.Render(k[1])
	}
	return ansi.Truncate(strings.Join(parts, "   "), w, "")
}

func (m Model) projectLabel(p gitlab.Pipeline) string {
	if rel, ok := strings.CutPrefix(p.ProjectPath, m.snap.Group.FullPath+"/"); ok {
		return rel
	}
	return p.ProjectPath
}

func refLabel(ref string) string {
	if rest, ok := strings.CutPrefix(ref, "refs/merge-requests/"); ok {
		iid, _, _ := strings.Cut(rest, "/")
		return "!" + iid
	}
	return ref
}

func (m Model) clock(t time.Time) string {
	t = t.Local()
	if y, mo, d := t.Date(); y == m.now.Year() && mo == m.now.Month() && d == m.now.Day() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 2 15:04")
}

func durationText(p gitlab.Pipeline, now time.Time) string {
	if d := p.Elapsed(now); d > 0 {
		return gitlab.FormatDuration(d)
	}
	if p.Category() == gitlab.CategoryQueued {
		return "queued"
	}
	return "–"
}

func ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	default:
		return t.Local().Format("Jan 2")
	}
}

func agoShort(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	if d := now.Sub(t); d < time.Minute {
		return fmt.Sprintf("%ds ago", max(int(d/time.Second), 0))
	}
	return ago(t, now)
}

func fit(s string, w int, right bool) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	pad := w - ansi.StringWidth(s)
	if pad <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", pad) + s
	}
	return s + strings.Repeat(" ", pad)
}

func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return ansi.Truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
