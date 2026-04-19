package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type openSpecChange struct {
	Name     string
	Done     int
	Total    int
	IsActive bool
}

type openSpecCourse struct {
	ID    string
	Name  string
	Done  int
	Total int
	Tasks []openSpecTask
}

type openSpecTask struct {
	ID   string
	Done bool
}

func initialOpenSpecRefreshCmd() tea.Cmd {
	return func() tea.Msg {
		return openSpecRefreshMsg(time.Now())
	}
}

func scheduleOpenSpecRefresh() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return openSpecRefreshMsg(t)
	})
}

func (m *Model) refreshOpenSpecData() error {
	out, err := exec.Command("openspec", "list", "--json").Output()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			m.openSpecStatusMessage = "(openspec not found)"
			m.openSpecChanges = nil
			m.openSpecCourses = nil
			return nil
		}
		m.openSpecStatusMessage = "(openspec unavailable)"
		m.openSpecChanges = nil
		m.openSpecCourses = nil
		return nil
	}

	changes, err := parseOpenSpecListOutput(out)
	if err != nil {
		m.openSpecStatusMessage = "(openspec unavailable)"
		m.openSpecChanges = nil
		m.openSpecCourses = nil
		return fmt.Errorf("parse openspec list output: %w", err)
	}

	m.openSpecChanges = changes
	m.openSpecStatusMessage = ""
	m.openSpecCourses = nil

	if len(m.openSpecChanges) == 0 {
		m.openSpecSelected = 0
		m.openSpecCourseSelected = 0
		return nil
	}
	if m.openSpecSelected >= len(m.openSpecChanges) {
		m.openSpecSelected = len(m.openSpecChanges) - 1
	}
	if m.openSpecSelected < 0 {
		m.openSpecSelected = 0
	}

	selected := m.openSpecChanges[m.openSpecSelected]
	tasksPath := filepath.Join("openspec", "changes", selected.Name, "tasks.md")
	courses, err := parseTasksMD(tasksPath)
	if err != nil {
		m.openSpecCourses = nil
		return nil
	}
	m.openSpecCourses = courses
	if len(m.openSpecCourses) == 0 {
		m.openSpecCourseSelected = 0
		return nil
	}
	if m.openSpecCourseSelected >= len(m.openSpecCourses) {
		m.openSpecCourseSelected = len(m.openSpecCourses) - 1
	}
	if m.openSpecCourseSelected < 0 {
		m.openSpecCourseSelected = 0
	}
	return nil
}

// cliChange mirrors the JSON shape returned by `openspec list --json`.
type cliChange struct {
	Name           string  `json:"name"`
	CompletedTasks *int    `json:"completedTasks"`
	TotalTasks     *int    `json:"totalTasks"`
	Status         string  `json:"status"`
	ArchivedAt     *string `json:"archived_at"`
}

func parseOpenSpecListOutput(data []byte) ([]openSpecChange, error) {
	type wrappedChanges struct {
		Changes []cliChange `json:"changes"`
	}

	var wrapped wrappedChanges
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Changes) > 0 {
		return normalizeOpenSpecChanges(wrapped.Changes), nil
	}

	var direct []cliChange
	if err := json.Unmarshal(data, &direct); err != nil {
		return nil, err
	}
	return normalizeOpenSpecChanges(direct), nil
}

func normalizeOpenSpecChanges(changes []cliChange) []openSpecChange {
	result := make([]openSpecChange, 0, len(changes))
	for _, change := range changes {
		done := 0
		if change.CompletedTasks != nil {
			done = *change.CompletedTasks
		}
		total := 0
		if change.TotalTasks != nil {
			total = *change.TotalTasks
		}
		result = append(result, openSpecChange{
			Name:     change.Name,
			Done:     done,
			Total:    total,
			IsActive: strings.EqualFold(change.Status, "in-progress") || strings.EqualFold(change.Status, "active") || change.ArchivedAt == nil,
		})
	}
	return result
}

func parseTasksMD(path string) ([]openSpecCourse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	courseRe := regexp.MustCompile(`(?m)^## Course (\S+): (.+)$`)
	taskRe := regexp.MustCompile(`(?m)^- \[([ x])\] (\S+)`)
	matches := courseRe.FindAllSubmatchIndex(data, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	courses := make([]openSpecCourse, 0, len(matches))
	for i, match := range matches {
		start := match[0]
		end := len(data)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		section := data[start:end]
		course := openSpecCourse{
			ID:   string(data[match[2]:match[3]]),
			Name: strings.TrimSpace(string(data[match[4]:match[5]])),
		}
		taskMatches := taskRe.FindAllSubmatch(section, -1)
		for _, taskMatch := range taskMatches {
			done := string(taskMatch[1]) == "x"
			course.Tasks = append(course.Tasks, openSpecTask{ID: string(taskMatch[2]), Done: done})
			course.Total++
			if done {
				course.Done++
			}
		}
		courses = append(courses, course)
	}

	return courses, nil
}

func (m Model) renderOpenSpecPanel(width, height int) string {
	if m.openSpecStatusMessage != "" {
		return mutedStyle.Render(m.openSpecStatusMessage)
	}
	if len(m.openSpecChanges) == 0 {
		return mutedStyle.Render("No active changes — run openspec propose")
	}
	if !m.openSpecExpanded {
		lines := make([]string, 0, len(m.openSpecChanges)+1)
		for i, change := range m.openSpecChanges {
			prefix := "  "
			if i == m.openSpecSelected {
				prefix = "> "
			}
			active := ""
			if change.IsActive {
				active = " ★"
			}
			pct := progressPercent(change.Done, change.Total)
			bar := progressBar(change.Done, change.Total, max(6, width-18))
			line := fmt.Sprintf("%s%s%s %s %d/%d %s", prefix, truncate(change.Name, max(1, width-18)), active, bar, change.Done, change.Total, pct)
			lines = append(lines, truncate(line, max(1, width-2)))
		}
		lines = append(lines, "", mutedStyle.Render("[↑/↓] navigate  [enter] expand  [ctrl+o] jump"))
		return strings.Join(lines, "\n")
	}

	lines := []string{truncate(m.openSpecChanges[m.openSpecSelected].Name+" — courses", max(1, width-2)), ""}
	if len(m.openSpecCourses) == 0 {
		lines = append(lines, mutedStyle.Render("(no tasks)"), "", mutedStyle.Render("[b] back"))
		return strings.Join(lines, "\n")
	}
	for i, course := range m.openSpecCourses {
		prefix := "  "
		if i == m.openSpecCourseSelected {
			prefix = "> "
		}
		pct := progressPercent(course.Done, course.Total)
		bar := progressBar(course.Done, course.Total, max(6, width-24))
		line := fmt.Sprintf("%s[%s] %s %s %d/%d %s", prefix, course.ID, truncate(course.Name, max(1, width-24)), bar, course.Done, course.Total, pct)
		lines = append(lines, truncate(line, max(1, width-2)))
	}
	lines = append(lines, "", mutedStyle.Render("[↑/↓] navigate  [b] back"))
	return strings.Join(lines, "\n")
}

func progressBar(done, total, width int) string {
	if width < 3 {
		width = 3
	}
	filledWidth := width
	if total > 0 {
		filledWidth = int(float64(width) * float64(done) / float64(total))
	}
	if filledWidth < 0 {
		filledWidth = 0
	}
	if filledWidth > width {
		filledWidth = width
	}
	return strings.Repeat("█", filledWidth) + strings.Repeat("░", width-filledWidth)
}

func progressPercent(done, total int) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (done*100)/total)
}

func isSidePanelKey(sidePanelIdx int, msg tea.KeyMsg) bool {
	if sidePanelIdx == int(SidePanelSnippets) {
		return isSnippetPanelKey(msg)
	}
	if sidePanelIdx == int(SidePanelOpenSpec) {
		switch msg.String() {
		case "up", "down", "enter", "b":
			return true
		default:
			return false
		}
	}
	return false
}

func (m *Model) refreshOpenSpecOnEntry() {
	if m.sidePanelIdx != int(SidePanelOpenSpec) {
		return
	}
	_ = m.refreshOpenSpecData()
	if len(m.openSpecChanges) == 0 {
		m.openSpecSelected = 0
		m.openSpecCourseSelected = 0
		m.openSpecExpanded = false
		return
	}
	if m.openSpecSelected >= len(m.openSpecChanges) {
		m.openSpecSelected = len(m.openSpecChanges) - 1
	}
	if m.openSpecSelected < 0 {
		m.openSpecSelected = 0
	}
	if len(m.openSpecCourses) == 0 {
		m.openSpecCourseSelected = 0
		return
	}
	if m.openSpecCourseSelected >= len(m.openSpecCourses) {
		m.openSpecCourseSelected = len(m.openSpecCourses) - 1
	}
	if m.openSpecCourseSelected < 0 {
		m.openSpecCourseSelected = 0
	}
}
