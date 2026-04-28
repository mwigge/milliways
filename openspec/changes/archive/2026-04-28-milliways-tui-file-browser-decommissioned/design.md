# Design — milliways-tui-file-browser

## 1. Data Model

### fileNode Type

```go
// internal/tui/file_browser.go

// fileNode represents a file or directory in the browser tree.
type fileNode struct {
    Name    string     // base name (e.g. "internal")
    Path    string     // full relative path from project root (e.g. "internal/tui")
    IsDir   bool
    Items   []*fileNode // nil for files, sorted children for dirs
    hidden  bool        // starts with .
}
```

### Model Fields

```go
// internal/tui/state.go — add to SidePanelMode enum
const (
    SidePanelLedger SidePanelMode = iota
    SidePanelJobs
    SidePanelCost
    SidePanelRouting
    SidePanelSystem
    SidePanelOpenSpec
    SidePanelSnippets
    SidePanelDiff
    SidePanelCompare
    SidePanelFile  // ← add
    sidePanelCount
)
```

```go
// internal/tui/app.go — add to Model
type Model struct {
    // ... existing fields ...
    
    // File browser state.
    fileBrowserRoot   string                          // project root (m.projectState.Root or ".")
    fileBrowserCursor int                             // selected index in flatView
    fileBrowserPath   []string                        // navigation stack (path segments)
    fileBrowserCache  map[string][]*fileNode         // path → cached children
}
```

## 2. Scanning

`scanDir(relPath string) []*fileNode` — reads a directory relative to project root:

```go
func scanDir(relPath string) []*fileNode {
    abs := filepath.Join(fileBrowserRoot, relPath)
    entries, err := os.ReadDir(abs)
    nodes := []*fileNode{}
    for _, entry := range entries {
        name := entry.Name()
        if name[0] == '.' && !showHidden {
            continue
        }
        isDir := entry.IsDir()
        node := &fileNode{
            Name:  name,
            Path:  filepath.Join(relPath, name),
            IsDir: isDir,
        }
        if isDir {
            node.Items = nil // lazy load on expand
        }
        nodes = append(nodes, node)
    }
    sort.Slice(nodes, func(i, j int) bool {
        // dirs first, then alphabetically
        if nodes[i].IsDir != nodes[j].IsDir {
            return nodes[i].IsDir
        }
        return nodes[i].Name < nodes[j].Name
    })
    return nodes
}
```

**Cache strategy:** cache is invalidated when cursor leaves/enters a directory. Cache key is the `relPath` string.

## 3. Flat View

For navigation, build a flat view of the current directory:

```go
// buildFlatView returns a flat list of visible items for cursor navigation.
func (m *Model) buildFlatView() []*fileNode {
    var path string
    if len(m.fileBrowserPath) == 0 {
        path = "."
    } else {
        path = filepath.Join(m.fileBrowserPath...)
    }
    children := m.getFileBrowserChildren(path)
    
    // Also include parent-dir ".." if not at root
    var flat []*fileNode
    if len(m.fileBrowserPath) > 0 {
        flat = append(flat, &fileNode{Name: "..", Path: filepath.Dir(path), IsDir: true})
    }
    flat = append(flat, children...)
    return flat
}

func (m *Model) getFileBrowserChildren(relPath string) []*fileNode {
    if cached, ok := m.fileBrowserCache[relPath]; ok {
        return cached
    }
    nodes := scanDir(relPath)
    m.fileBrowserCache[relPath] = nodes
    return nodes
}
```

## 4. Key Handling

In `handleKey()`, add to `tea.KeyRunes` case for `SidePanelFile` mode. Also handle arrow keys:

```go
// Arrow keys in file browser panel
case tea.KeyRight, tea.KeyLeft:
    if m.sidePanelIdx == int(SidePanelFile) && !m.overlayActive {
        flat := m.buildFlatView()
        if m.fileBrowserCursor < len(flat) {
            node := flat[m.fileBrowserCursor]
            if msg.Type == tea.KeyRight && node.IsDir {
                // Enter directory
                m.fileBrowserPath = append(m.fileBrowserPath, node.Name)
                m.fileBrowserCursor = 0
                m.fileBrowserCache = make(map[string][]*fileNode) // invalidate cache
                return nil
            }
            if msg.Type == tea.KeyLeft && len(m.fileBrowserPath) > 0 {
                // Go up one level
                m.fileBrowserPath = m.fileBrowserPath[:len(m.fileBrowserPath)-1]
                m.fileBrowserCursor = 0
                m.fileBrowserCache = make(map[string][]*fileNode)
                return nil
            }
        }
    }

// isSidePanelKey — add SidePanelFile to the panel key check
// (already handles ↑/↓ via isSidePanelKey in the arrow key case)
```

```go
case "enter":
    // File browser: insert path into prompt
    if m.sidePanelIdx == int(SidePanelFile) && !m.overlayActive {
        flat := m.buildFlatView()
        if m.fileBrowserCursor < len(flat) {
            node := flat[m.fileBrowserCursor]
            if !node.IsDir {
                // Insert relative path into prompt
                relPath := node.Path
                current := m.input.Value()
                if current != "" && !strings.HasSuffix(current, " ") {
                    relPath = " " + relPath
                }
                m.input.SetValue(current + relPath)
            }
        }
        return nil
    }
```

## 5. Rendering

```go
// internal/tui/view.go

func renderFileBrowserPanel(width, height int, m *Model) string {
    flat := m.buildFlatView()
    if len(flat) == 0 {
        return lipgloss.NewStyle().
            Width(width).
            Height(height).
            Render("  (empty directory)")
    }

    // Current path breadcrumb
    var breadcrumb string
    if len(m.fileBrowserPath) == 0 {
        breadcrumb = "."
    } else {
        breadcrumb = filepath.Join(m.fileBrowserPath...)
    }

    var lines []string
    lines = append(lines, lipgloss.NewStyle().
        Foreground(lipgloss.Color("#F59E0B")).
        Render("📁 "+breadcrumb))
    lines = append(lines, "")

    for i, node := range flat {
        prefix := "  "
        if i == m.fileBrowserCursor {
            prefix = "▶ "
        }
        icon := "📄"
        if node.IsDir {
            icon = "📂"
            if node.Name == ".." {
                icon = "↩ "
            }
        }
        indent := ""
        if node.Name != ".." {
            indent = "   "
        }
        line := prefix + icon + " " + indent + node.Name
        if i == m.fileBrowserCursor {
            line = lipgloss.NewStyle().
                Background(lipgloss.Color("#374151")).
                Render(line)
        }
        lines = append(lines, line)
    }

    content := strings.Join(lines, "\n")
    return lipgloss.NewStyle().
        Width(width).
        Height(height).
        Render(content)
}
```

## 6. File Browser Entry Refresh

On entering the file browser panel (via `advanceSidePanel`/`rewindSidePanel`), refresh the cache:

```go
// In rewindSidePanel/advanceSidePanel or in the panel rendering path:
if m.sidePanelIdx == int(SidePanelFile) {
    m.fileBrowserCache = make(map[string][]*fileNode)
    m.fileBrowserCursor = 0
}
```

## 7. File Manifest

| File | Changes |
|------|---------|
| `internal/tui/state.go` | Add `SidePanelFile` to `SidePanelMode` enum |
| `internal/tui/app.go` | Add `fileBrowserRoot`, `fileBrowserCursor`, `fileBrowserPath`, `fileBrowserCache` fields to `Model` |
| `internal/tui/app.go` | Initialize file browser fields in `NewModel()` |
| `internal/tui/app.go` | Arrow key handling in `handleKey` for `SidePanelFile` |
| `internal/tui/app.go` | Enter key handling: insert path into prompt |
| `internal/tui/file_browser.go` | **NEW** — `fileNode` type, `scanDir`, `buildFlatView`, `getFileBrowserChildren` |
| `internal/tui/view.go` | Add `renderFileBrowserPanel` function |
| `internal/tui/view.go` | Add `SidePanelFile` case to `renderActiveSidePanel` dispatch |
| `internal/tui/panels_test.go` | Add file browser panel cycling test |
| `internal/tui/file_browser_test.go` | **NEW** — tests for scanDir, buildFlatView, navigation |

## 8. Dependencies

None beyond stdlib: `os`, `path/filepath`, `sort`. No external packages required.
