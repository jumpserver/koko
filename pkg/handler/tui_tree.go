package handler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/i18n"
)

type assetTUITreeKind uint8

const (
	assetTUIAuthorizationTree assetTUITreeKind = iota + 1
	assetTUITypeTree
	assetTUIFavoriteTree
)

type assetTUITreeNode struct {
	identity       string
	id             string
	key            string
	parent         string
	name           string
	kind           string
	category       string
	assetType      string
	count          int
	countLoaded    bool
	countFailed    bool
	hasChildren    bool
	childrenLoaded bool
	loading        bool
	expanded       bool
	children       []string
}

type assetTUITreeCache struct {
	kind        assetTUITreeKind
	generation  int
	initialized bool
	loading     bool
	err         error
	roots       []string
	nodes       map[string]*assetTUITreeNode
}

type assetTUITreeDialog struct {
	kind              assetTUITreeKind
	cursor            int
	scroll            int
	scrollbarDragging bool
	scrollbarGrab     int
	lastClickRow      int
	lastClickAt       time.Time
}

type assetTUITreeNodesMsg struct {
	kind       assetTUITreeKind
	generation int
	parent     string
	nodes      []assetTUITreeNode
	err        error
}

type assetTUITreeCountsMsg struct {
	kind       assetTUITreeKind
	generation int
	ids        []string
	counts     map[string]int
	err        error
}

type assetTUITreeAPIItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Parent      string `json:"pId"`
	HasChildren bool   `json:"hasChildren"`
	Meta        struct {
		Type      string `json:"type"`
		Category  string `json:"category"`
		AssetType string `json:"_type"`
		Data      struct {
			ID          string `json:"id"`
			Key         string `json:"key"`
			Value       string `json:"value"`
			HasChildren bool   `json:"has_children"`
		} `json:"data"`
	} `json:"meta"`
}

type assetTUITreePage struct {
	Results        []assetTUITreeAPIItem `json:"results"`
	NodePagination struct {
		HasMore bool   `json:"has_more"`
		Next    string `json:"next"`
	} `json:"node_pagination"`
}

func (p *assetTUITreePage) UnmarshalJSON(data []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		return json.Unmarshal(data, &p.Results)
	}
	type page assetTUITreePage
	return json.Unmarshal(data, (*page)(p))
}

type assetTUITreeRow struct {
	id    string
	depth int
}

func (m *assetTUI) treeCache(kind assetTUITreeKind) *assetTUITreeCache {
	if m.trees == nil {
		m.trees = make(map[assetTUITreeKind]*assetTUITreeCache)
	}
	cache := m.trees[kind]
	if cache == nil {
		cache = &assetTUITreeCache{kind: kind, nodes: make(map[string]*assetTUITreeNode)}
		m.trees[kind] = cache
	}
	return cache
}

func (m *assetTUI) openTreeDialog(kind assetTUITreeKind) (tea.Model, tea.Cmd) {
	if m.treeDialog != nil {
		m.rememberTreeDialog()
	}
	cache := m.treeCache(kind)
	rows := cache.visibleRows()
	dialog := &assetTUITreeDialog{kind: kind, lastClickRow: -1}
	restored := false
	if state, ok := m.treeDialogStates[kind]; ok {
		*dialog = state
		dialog.kind = kind
		dialog.scrollbarDragging = false
		dialog.scrollbarGrab = 0
		dialog.lastClickRow = -1
		dialog.lastClickAt = time.Time{}
		restored = true
	} else {
		for i := range rows {
			if kind == m.selectedTree && rows[i].id == m.selectedTreeID {
				dialog.cursor = i
				break
			}
		}
	}
	m.treeDialog = dialog
	if restored {
		m.ensureTreeCursorVisible(len(rows))
	} else {
		dialog.scroll = tuiDialogListStart(dialog.cursor, len(rows), m.treeDialogGeometry().rows)
	}
	if cache.initialized || cache.loading {
		return m, nil
	}
	return m, m.refreshTree(kind)
}

func (m *assetTUI) reopenTreeDialog() (tea.Model, tea.Cmd) {
	kind := m.lastTreeDialog
	if kind == 0 {
		kind = assetTUIAuthorizationTree
	}
	return m.openTreeDialog(kind)
}

func (m *assetTUI) rememberTreeDialog() {
	if m.treeDialog == nil {
		return
	}
	if m.treeDialogStates == nil {
		m.treeDialogStates = make(map[assetTUITreeKind]assetTUITreeDialog)
	}
	state := *m.treeDialog
	state.scrollbarDragging = false
	state.scrollbarGrab = 0
	state.lastClickRow = -1
	state.lastClickAt = time.Time{}
	m.treeDialogStates[state.kind] = state
	m.lastTreeDialog = state.kind
}

func (m *assetTUI) closeTreeDialog() {
	m.rememberTreeDialog()
	m.treeDialog = nil
}

func (m *assetTUI) refreshTree(kind assetTUITreeKind) tea.Cmd {
	cache := m.treeCache(kind)
	cache.generation++
	cache.initialized = false
	cache.loading = true
	cache.err = nil
	cache.roots = nil
	cache.nodes = make(map[string]*assetTUITreeNode)
	if m.treeDialog != nil && m.treeDialog.kind == kind {
		m.treeDialog.cursor = 0
	}
	return m.loadTreeNodes(kind, cache.generation, "")
}

func (m *assetTUI) loadTreeNodes(kind assetTUITreeKind, generation int, parent string) tea.Cmd {
	data := classicData{api: m.handler.jmsService, userID: m.selector.user.ID, lang: m.handler.i18nLang}
	return func() tea.Msg {
		var nodes []assetTUITreeNode
		var err error
		switch kind {
		case assetTUIAuthorizationTree:
			nodes, err = data.tuiAuthorizationNodes(parent)
		case assetTUITypeTree:
			nodes, err = data.tuiTypeNodes()
		case assetTUIFavoriteTree:
			nodes, err = data.tuiFavoriteNodes(parent)
		}
		return assetTUITreeNodesMsg{
			kind: kind, generation: generation, parent: parent, nodes: nodes, err: err,
		}
	}
}

func (m *assetTUI) loadTreeCounts(kind assetTUITreeKind, generation int, ids []string) tea.Cmd {
	if kind == assetTUITypeTree || len(ids) == 0 {
		return nil
	}
	data := classicData{api: m.handler.jmsService, userID: m.selector.user.ID, lang: m.handler.i18nLang}
	return func() tea.Msg {
		counts := make(map[string]int, len(ids))
		org, mode := "", 0
		if kind == assetTUIFavoriteTree {
			org, mode = "ROOT", 2
		}
		for start := 0; start < len(ids); start += classicTreeBatchSize {
			end := min(start+classicTreeBatchSize, len(ids))
			page, err := data.nodeCounts(org, mode, ids[start:end])
			if err != nil {
				return assetTUITreeCountsMsg{kind: kind, generation: generation, ids: ids, err: err}
			}
			for id, count := range page {
				counts[id] = count
			}
		}
		return assetTUITreeCountsMsg{kind: kind, generation: generation, ids: ids, counts: counts}
	}
}

func (m *assetTUI) updateTreeNodes(msg assetTUITreeNodesMsg) tea.Cmd {
	cache := m.treeCache(msg.kind)
	if cache.generation != msg.generation {
		return nil
	}
	if msg.parent == "" {
		cache.loading = false
	} else if parent := cache.nodes[msg.parent]; parent != nil {
		parent.loading = false
	}
	if msg.err != nil {
		cache.err = msg.err
		if parent := cache.nodes[msg.parent]; parent != nil {
			parent.expanded = false
		}
		return nil
	}
	cache.err = nil
	metricIDs := cache.addNodes(msg.parent, msg.nodes)
	var commands []tea.Cmd
	if cmd := m.loadTreeCounts(msg.kind, msg.generation, metricIDs); cmd != nil {
		commands = append(commands, cmd)
	}
	if msg.parent == "" {
		cache.initialized = true
		if len(cache.roots) == 1 {
			root := cache.nodes[cache.roots[0]]
			root.expanded = true
			if root.hasChildren && !root.childrenLoaded && !root.loading {
				root.loading = true
				commands = append(commands, m.loadTreeNodes(msg.kind, msg.generation, root.identity))
			}
		}
	}
	m.clampTreeCursor()
	return tea.Batch(commands...)
}

func (m *assetTUI) updateTreeCounts(msg assetTUITreeCountsMsg) {
	cache := m.treeCache(msg.kind)
	if cache.generation != msg.generation {
		return
	}
	byMetricID := make(map[string]*assetTUITreeNode, len(cache.nodes))
	for _, node := range cache.nodes {
		byMetricID[node.id] = node
	}
	if msg.err != nil {
		cache.err = msg.err
		for _, id := range msg.ids {
			if node := byMetricID[id]; node != nil {
				node.countLoaded = true
				node.countFailed = true
			}
		}
		return
	}
	for _, id := range msg.ids {
		if node := byMetricID[id]; node != nil {
			node.count = msg.counts[id]
			node.countLoaded = true
			node.countFailed = false
		}
	}
}

func (c *assetTUITreeCache) addNodes(parent string, nodes []assetTUITreeNode) []string {
	if c.nodes == nil {
		c.nodes = make(map[string]*assetTUITreeNode)
	}
	if parent == "" {
		c.roots = nil
	} else if current := c.nodes[parent]; current != nil {
		current.children = nil
		current.childrenLoaded = true
		current.hasChildren = len(nodes) > 0
	}
	metricIDs := make([]string, 0, len(nodes))
	for i := range nodes {
		node := nodes[i]
		if existing := c.nodes[node.identity]; existing != nil {
			node.expanded = existing.expanded
			node.children = existing.children
			node.childrenLoaded = existing.childrenLoaded
		}
		copyNode := node
		c.nodes[node.identity] = &copyNode
		if node.id != "" && !node.countLoaded {
			metricIDs = append(metricIDs, node.id)
		}
	}

	for i := range nodes {
		node := c.nodes[nodes[i].identity]
		if node.parent == "" || c.nodes[node.parent] == nil {
			c.roots = appendUnique(c.roots, node.identity)
			continue
		}
		parentNode := c.nodes[node.parent]
		parentNode.children = appendUnique(parentNode.children, node.identity)
		parentNode.hasChildren = true
	}

	switch c.kind {
	case assetTUITypeTree:
		for _, node := range c.nodes {
			node.childrenLoaded = true
			node.hasChildren = len(node.children) > 0
		}
		if root := c.nodes["ROOT"]; root != nil {
			root.count = 0
			for _, childID := range root.children {
				root.count += c.nodes[childID].count
			}
			root.countLoaded = true
		}
	case assetTUIFavoriteTree:
		if parent == "" && len(c.roots) == 1 {
			root := c.nodes[c.roots[0]]
			root.childrenLoaded = true
			root.hasChildren = len(root.children) > 0
		}
	}
	return metricIDs
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (c *assetTUITreeCache) visibleRows() []assetTUITreeRow {
	rows := make([]assetTUITreeRow, 0, len(c.nodes))
	seen := make(map[string]struct{}, len(c.nodes))
	var walk func(string, int)
	walk = func(id string, depth int) {
		if _, ok := seen[id]; ok {
			return
		}
		node := c.nodes[id]
		if node == nil {
			return
		}
		seen[id] = struct{}{}
		rows = append(rows, assetTUITreeRow{id: id, depth: depth})
		if !node.expanded {
			return
		}
		for _, child := range node.children {
			walk(child, depth+1)
		}
	}
	for _, root := range c.roots {
		walk(root, 0)
	}
	return rows
}

func (c *assetTUITreeCache) path(id string) string {
	parts := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for id != "" {
		if _, ok := seen[id]; ok {
			break
		}
		seen[id] = struct{}{}
		node := c.nodes[id]
		if node == nil {
			break
		}
		parts = append(parts, node.name)
		id = node.parent
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return "/" + strings.Join(parts, "/")
}

func (m *assetTUI) updateTreeDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := m.treeDialog
	cache := m.treeCache(dialog.kind)
	rows := cache.visibleRows()
	dialog.lastClickRow = -1
	dialog.lastClickAt = time.Time{}
	if tuiIsSpaceKey(msg) {
		m.openTreeNodeDetail(rows)
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.closeTreeDialog()
	case tea.KeyTab:
		next := dialog.kind + 1
		if next > assetTUIFavoriteTree {
			next = assetTUIAuthorizationTree
		}
		return m.openTreeDialog(next)
	case tea.KeyShiftTab:
		previous := dialog.kind - 1
		if previous < assetTUIAuthorizationTree {
			previous = assetTUIFavoriteTree
		}
		return m.openTreeDialog(previous)
	case tea.KeyUp:
		dialog.cursor = max(0, dialog.cursor-1)
	case tea.KeyDown:
		dialog.cursor = min(max(0, len(rows)-1), dialog.cursor+1)
	case tea.KeyHome:
		dialog.cursor = 0
	case tea.KeyEnd:
		dialog.cursor = max(0, len(rows)-1)
	case tea.KeyLeft:
		m.collapseTreeNode(rows)
	case tea.KeyRight:
		cmd := m.expandTreeNode(rows)
		m.ensureTreeCursorVisible(len(cache.visibleRows()))
		return m, cmd
	case tea.KeyEnter:
		return m.chooseTreeNode(rows)
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, nil
		}
		switch msg.Runes[0] {
		case 'k':
			dialog.cursor = max(0, dialog.cursor-1)
		case 'j':
			dialog.cursor = min(max(0, len(rows)-1), dialog.cursor+1)
		case 'h':
			m.collapseTreeNode(rows)
		case 'l':
			cmd := m.expandTreeNode(rows)
			m.ensureTreeCursorVisible(len(cache.visibleRows()))
			return m, cmd
		case 'r', 'R':
			return m, m.refreshTree(dialog.kind)
		}
	}
	m.ensureTreeCursorVisible(len(cache.visibleRows()))
	return m, nil
}

func (m *assetTUI) collapseTreeNode(rows []assetTUITreeRow) {
	if m.treeDialog == nil || m.treeDialog.cursor < 0 || m.treeDialog.cursor >= len(rows) {
		return
	}
	cache := m.treeCache(m.treeDialog.kind)
	node := cache.nodes[rows[m.treeDialog.cursor].id]
	if node.expanded {
		node.expanded = false
		return
	}
	if node.parent == "" {
		return
	}
	for index := range rows {
		if rows[index].id == node.parent {
			m.treeDialog.cursor = index
			return
		}
	}
}

func (m *assetTUI) expandTreeNode(rows []assetTUITreeRow) tea.Cmd {
	if m.treeDialog == nil || m.treeDialog.cursor < 0 || m.treeDialog.cursor >= len(rows) {
		return nil
	}
	cache := m.treeCache(m.treeDialog.kind)
	node := cache.nodes[rows[m.treeDialog.cursor].id]
	if node.expanded {
		if len(node.children) > 0 {
			m.treeDialog.cursor++
		}
		return nil
	}
	if !node.hasChildren {
		return nil
	}
	node.expanded = true
	if node.childrenLoaded || node.loading {
		return nil
	}
	node.loading = true
	cache.err = nil
	return m.loadTreeNodes(cache.kind, cache.generation, node.identity)
}

func (m *assetTUI) chooseTreeNode(rows []assetTUITreeRow) (tea.Model, tea.Cmd) {
	if m.loading || m.treeDialog == nil || m.treeDialog.cursor < 0 || m.treeDialog.cursor >= len(rows) {
		return m, nil
	}
	cache := m.treeCache(m.treeDialog.kind)
	node := cache.nodes[rows[m.treeDialog.cursor].id]
	path := cache.path(node.identity)
	switch cache.kind {
	case assetTUIAuthorizationTree:
		m.handler.nodes = m.authorizationTreeNodes(cache)
		m.selector.SetNode(model.Node{
			ID: node.id, Key: node.key, Name: node.name, Parent: node.parent,
			AssetsAmount: node.count,
		})
		m.selector.selectedPath = path
		m.handler.treeOrigin = TypeNodeAsset
	case assetTUITypeTree:
		if node.identity == "ROOT" {
			m.selector.SetSelectType(TypeAsset)
		} else {
			m.selector.SetType(classicTypeNode{
				ID: node.id, Parent: node.parent, Name: node.name, Kind: node.kind,
				Category: node.category, AssetType: node.assetType, PlatformID: node.id,
				AssetsAmount: node.count, Path: path,
			})
		}
		m.handler.treeOrigin = TypeTypeAsset
	case assetTUIFavoriteTree:
		m.selector.SetFavorite(classicFavoriteNode{
			ID: node.id, Key: node.key, Parent: node.parent, Name: node.name,
			AssetsAmount: node.count, Path: path,
		})
		m.handler.treeOrigin = TypeFavoriteAsset
	}
	m.handler.treeSelected = true
	m.selectedTree = cache.kind
	m.selectedTreeID = node.identity
	m.selectedPath = path
	m.query = ""
	m.searchInput = nil
	m.searching = false
	m.closeTreeDialog()
	return m, m.loadPage(0)
}

func (m *assetTUI) authorizationTreeNodes(cache *assetTUITreeCache) model.NodeList {
	nodes := make(model.NodeList, 0, len(cache.nodes))
	for _, node := range cache.nodes {
		nodes = append(nodes, model.Node{
			ID: node.id, Key: node.key, Name: node.name, Parent: node.parent,
			AssetsAmount: node.count,
		})
	}
	return nodes
}

func (m *assetTUI) clampTreeCursor() {
	if m.treeDialog == nil {
		return
	}
	rows := m.treeCache(m.treeDialog.kind).visibleRows()
	m.treeDialog.cursor = max(0, min(m.treeDialog.cursor, len(rows)-1))
	m.ensureTreeCursorVisible(len(rows))
}

func (m *assetTUI) ensureTreeCursorVisible(total int) int {
	if m.treeDialog == nil {
		return 0
	}
	dialog := m.treeDialog
	visible := max(1, m.treeDialogGeometry().rows)
	dialog.cursor = max(0, min(dialog.cursor, total-1))
	dialog.scroll = max(0, min(dialog.scroll, max(0, total-visible)))
	if dialog.cursor < dialog.scroll {
		dialog.scroll = dialog.cursor
	} else if dialog.cursor >= dialog.scroll+visible {
		dialog.scroll = dialog.cursor - visible + 1
	}
	return dialog.scroll
}

func (m *assetTUI) treeTitle(kind assetTUITreeKind) string {
	lang := i18n.NewLang(m.handler.i18nLang)
	switch kind {
	case assetTUIAuthorizationTree:
		return lang.T("Authorization tree")
	case assetTUITypeTree:
		return lang.T("Type tree")
	case assetTUIFavoriteTree:
		return lang.T("Favorite tree")
	default:
		return ""
	}
}

func (m *assetTUI) treeDialogGeometry() assetTUIDialogGeometry {
	width := min(70, max(1, m.width-10))
	maxHeight := max(1, m.height-6)
	height := min(max(7, (m.height*2+2)/3), maxHeight)
	return assetTUIDialogGeometry{
		x: (m.width - width) / 2, y: (m.height - height) / 2,
		width: width, height: height, rows: max(1, height-6),
	}
}

func (m *assetTUI) treeDialogTitle(selected assetTUITreeKind, width int) string {
	if width <= 0 {
		return ""
	}
	labels := []string{
		m.treeTitle(assetTUIAuthorizationTree),
		m.treeTitle(assetTUITypeTree),
		m.treeTitle(assetTUIFavoriteTree),
	}
	separator := "│"
	if width < len(labels)-1 {
		return strings.Repeat(separator, width)
	}
	available := width - len(labels) + 1
	labelWidth := available / len(labels)
	remaining := available % len(labels)
	visible := make([]string, len(labels))
	for i, label := range labels {
		cellWidth := labelWidth
		if i < remaining {
			cellWidth++
		}
		highlightPadding := 0
		if assetTUITreeKind(i+1) == selected {
			highlightPadding = min(2, max(0, (cellWidth-1)/2))
		}
		label = runewidth.Truncate(label, cellWidth-highlightPadding*2, "…")
		padding := max(0, cellWidth-runewidth.StringWidth(label)-highlightPadding*2)
		left := padding / 2
		right := padding - left
		if highlightPadding > 0 {
			label = tuiSelectedStyle + strings.Repeat(" ", highlightPadding) + label +
				strings.Repeat(" ", highlightPadding) + tuiStyleReset
		}
		visible[i] = strings.Repeat(" ", left) + label + strings.Repeat(" ", right)
	}
	return strings.Join(visible, separator)
}

func treeDialogKindAtColumn(column, width int) (assetTUITreeKind, bool) {
	const treeCount = int(assetTUIFavoriteTree)
	if column < 0 || width < treeCount-1 || column >= width {
		return 0, false
	}
	available := width - treeCount + 1
	cellWidth := available / treeCount
	remaining := available % treeCount
	start := 0
	for index := 0; index < treeCount; index++ {
		currentWidth := cellWidth
		if index < remaining {
			currentWidth++
		}
		if column >= start && column < start+currentWidth {
			return assetTUITreeKind(index + 1), true
		}
		start += currentWidth
		if index+1 < treeCount {
			if column == start {
				return 0, false
			}
			start++
		}
	}
	return 0, false
}

func (m *assetTUI) renderTreeDialog(lines []string) {
	geometry := m.treeDialogGeometry()
	if geometry.width < 24 || geometry.height < 7 || geometry.y < 0 || geometry.y+geometry.height > len(lines) {
		return
	}
	dialog := m.treeDialog
	cache := m.treeCache(dialog.kind)
	rows := cache.visibleRows()
	popup := tuiDialogFrame("", geometry.width, geometry.height)
	popup[1] = "│" + m.treeDialogTitle(dialog.kind, geometry.width-2) + "│"
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	start := m.ensureTreeCursorVisible(len(rows))
	showScrollbar := len(rows) > geometry.rows
	rowWidth := geometry.width - 2
	if showScrollbar {
		rowWidth--
	}
	for row := 0; row < geometry.rows; row++ {
		content := ""
		position := start + row
		if position < len(rows) {
			item := rows[position]
			node := cache.nodes[item.id]
			icon := "  "
			if node.loading {
				icon = "… "
			} else if node.hasChildren {
				icon = "▸ "
				if node.expanded {
					icon = "▾ "
				}
			}
			count := " (…)"
			if node.countFailed {
				count = " (?)"
			} else if node.countLoaded {
				count = fmt.Sprintf(" (%d)", node.count)
			}
			labelWidth := rowWidth - 4 - item.depth*2
			label := strings.Repeat("  ", item.depth) + icon +
				runewidth.Truncate(node.name, max(1, labelWidth-runewidth.StringWidth(count)), "") + count
			content = tuiDialogLeft("  "+label, rowWidth)
			if position == dialog.cursor {
				content = tuiSelectedStyle + content + tuiStyleReset
			}
		} else if row == 0 {
			switch {
			case cache.loading:
				content = tuiDialogLeft(m.tr("正在加载树…", "Loading tree…"), rowWidth)
			case cache.err != nil:
				content = tuiDialogLeft(userFacingErrorMessage(m.tr("树加载失败", "Failed to load tree"), cache.err), rowWidth)
			case cache.initialized:
				content = tuiDialogLeft(i18n.NewLang(m.handler.i18nLang).T("No nodes"), rowWidth)
			}
		}
		if content == "" {
			content = strings.Repeat(" ", rowWidth)
		}
		scrollbar := ""
		if showScrollbar {
			scrollbar = tuiScrollbarCell(row, start, len(rows), geometry.rows)
		}
		popup[row+3] = "│" + content + scrollbar + "│"
	}
	lang := i18n.NewLang(m.handler.i18nLang)
	shortcuts := tuiShortcutLine(geometry.width-4, []string{
		"space:" + lang.T("Details"), "tab:" + lang.T("Asset tree"),
		"esc:" + lang.T("Cancel"), "enter:" + lang.T("Confirm"),
	}, "?:"+lang.T("View help"))
	popup[geometry.height-3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[geometry.height-2] = "│" + tuiDialogLeft(shortcuts, geometry.width-2) + "│"
	m.overlayDialog(lines, popup, geometry)
}

func (m *assetTUI) updateTreeDialogMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	geometry := m.treeDialogGeometry()
	dialog := m.treeDialog
	rows := m.treeCache(dialog.kind).visibleRows()
	listY := geometry.y + 3
	if dialog.scrollbarDragging {
		if event.Action == tea.MouseActionRelease {
			dialog.scrollbarDragging = false
			return m, nil
		}
		if event.Action == tea.MouseActionMotion {
			row := max(0, min(event.Y-listY, geometry.rows-1))
			start := tuiScrollbarStartAt(row, dialog.scrollbarGrab, len(rows), geometry.rows)
			dialog.scroll = start
			dialog.cursor = min(start, len(rows)-1)
		}
		return m, nil
	}
	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if event.X < geometry.x || event.X >= geometry.x+geometry.width ||
		event.Y < geometry.y || event.Y >= geometry.y+geometry.height {
		if event.Button == tea.MouseButtonLeft {
			m.closeTreeDialog()
		}
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelUp || event.Button == tea.MouseButtonWheelDown {
		step := 1
		if event.Button == tea.MouseButtonWheelUp {
			step = -1
		}
		dialog.cursor = max(0, min(len(rows)-1, dialog.cursor+step))
		m.ensureTreeCursorVisible(len(rows))
		return m, nil
	}
	if event.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if event.Y == geometry.y+1 {
		kind, ok := treeDialogKindAtColumn(event.X-geometry.x-1, geometry.width-2)
		if !ok || kind == dialog.kind {
			return m, nil
		}
		return m.openTreeDialog(kind)
	}
	if len(rows) > geometry.rows && event.X == geometry.x+geometry.width-2 &&
		event.Y >= listY && event.Y < listY+geometry.rows {
		start := m.ensureTreeCursorVisible(len(rows))
		thumbStart, thumbSize, _, _ := tuiScrollbarMetrics(start, len(rows), geometry.rows)
		row := event.Y - listY
		dialog.scrollbarGrab = thumbSize / 2
		if row >= thumbStart && row < thumbStart+thumbSize {
			dialog.scrollbarGrab = row - thumbStart
		}
		dialog.scrollbarDragging = true
		newStart := tuiScrollbarStartAt(row, dialog.scrollbarGrab, len(rows), geometry.rows)
		if newStart != start {
			dialog.scroll = newStart
			dialog.cursor = newStart
		}
		dialog.lastClickRow = -1
		dialog.lastClickAt = time.Time{}
		return m, nil
	}
	row := event.Y - geometry.y - 3
	if row < 0 || row >= geometry.rows {
		dialog.lastClickRow = -1
		dialog.lastClickAt = time.Time{}
		return m, nil
	}
	start := m.ensureTreeCursorVisible(len(rows))
	position := start + row
	if position >= len(rows) {
		return m, nil
	}
	dialog.cursor = position
	dialog.scroll = start
	node := m.treeCache(dialog.kind).nodes[rows[position].id]
	arrowStart := geometry.x + 5 + rows[position].depth*2
	if (node.hasChildren || node.expanded || node.loading) &&
		event.X >= geometry.x+1 && event.X <= arrowStart+1 {
		dialog.lastClickRow = -1
		dialog.lastClickAt = time.Time{}
		var cmd tea.Cmd
		if node.expanded {
			node.expanded = false
		} else {
			cmd = m.expandTreeNode(rows)
		}
		m.ensureTreeCursorVisible(len(m.treeCache(dialog.kind).visibleRows()))
		return m, cmd
	}
	now := time.Now()
	if dialog.lastClickRow == position && now.Sub(dialog.lastClickAt) <= tuiDoubleClickInterval {
		return m.chooseTreeNode(rows)
	}
	dialog.lastClickRow = position
	dialog.lastClickAt = now
	return m, nil
}

func (d classicData) tuiAuthorizationNodes(parent string) ([]assetTUITreeNode, error) {
	path := d.userPath("nodes/children-with-assets/tree/")
	client := newLangAPIClient(d.api, d.lang)
	params := map[string]string{
		"include_assets": "false", "node_page_size": strconv.Itoa(classicTreeBatchSize),
	}
	if parent != "" {
		params["parent_key"] = parent
	}
	items := make([]assetTUITreeAPIItem, 0, classicTreeBatchSize)
	seenCursors := make(map[string]struct{})
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > classicTreeLimit/classicTreeBatchSize {
			return nil, fmt.Errorf("authorization tree exceeds %d nodes", classicTreeLimit)
		}
		var page assetTUITreePage
		_, err := client.Call("GET", path, nil, &page, params)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Results...)
		if !page.NodePagination.HasMore {
			break
		}
		nextURL, err := url.Parse(page.NodePagination.Next)
		if err != nil {
			return nil, fmt.Errorf("authorization tree response has an invalid next cursor")
		}
		cursor := nextURL.Query().Get("node_cursor")
		if cursor == "" {
			return nil, fmt.Errorf("authorization tree response is missing next cursor")
		}
		if _, ok := seenCursors[cursor]; ok {
			return nil, fmt.Errorf("authorization tree response repeated its next cursor")
		}
		seenCursors[cursor] = struct{}{}
		params["node_cursor"] = cursor
	}
	if len(items) > classicTreeLimit {
		return nil, fmt.Errorf("authorization tree exceeds %d nodes", classicTreeLimit)
	}
	nodes := make([]assetTUITreeNode, 0, len(items))
	for _, item := range items {
		if item.Meta.Type != "node" {
			continue
		}
		id, key := strings.TrimSpace(item.Meta.Data.ID), strings.TrimSpace(item.Meta.Data.Key)
		if key == "" {
			key = strings.TrimSpace(item.ID)
		}
		if id == "" {
			id = key
		}
		name := strings.TrimSpace(item.Meta.Data.Value)
		if name == "" {
			name = strings.TrimSpace(item.Name)
		}
		if id == "" || key == "" || name == "" {
			return nil, fmt.Errorf("authorization tree response is missing id, key, or name")
		}
		nodes = append(nodes, assetTUITreeNode{
			identity: key, id: id, key: key, parent: item.Parent, name: name,
			kind: "node", hasChildren: true,
		})
	}
	return nodes, nil
}

func (d classicData) tuiFavoriteNodes(parent string) ([]assetTUITreeNode, error) {
	client := newLangAPIClient(d.api, d.lang)
	params := map[string]string{"include_assets": "false"}
	if parent != "" {
		parentNodeID := strings.TrimPrefix(parent, "favorite-folder:")
		params["parent_id"] = parentNodeID
	}
	var page assetTUITreePage
	_, err := client.Call("GET", d.userPath("favorite-tree/"), nil, &page, params)
	if err != nil {
		return nil, err
	}
	if len(page.Results) > classicTreeLimit {
		return nil, fmt.Errorf("favorite tree exceeds %d folders", classicTreeLimit)
	}
	nodes := make([]assetTUITreeNode, 0, len(page.Results))
	for _, item := range page.Results {
		if item.Meta.Type != "node" {
			continue
		}
		id, key := strings.TrimSpace(item.Meta.Data.ID), strings.TrimSpace(item.Meta.Data.Key)
		if key == "" {
			key = strings.TrimSpace(item.ID)
		}
		name := strings.TrimSpace(item.Meta.Data.Value)
		if name == "" {
			name = strings.TrimSpace(item.Name)
		}
		if id == "" || key == "" || name == "" {
			return nil, fmt.Errorf("favorite tree response is missing id, key, or name")
		}
		if id == "favorite-root" {
			name = i18n.NewLang(d.lang).T("All favorites")
		}
		nodes = append(nodes, assetTUITreeNode{
			identity: key, id: id, key: key, parent: item.Parent, name: name,
			kind: "node", hasChildren: item.HasChildren || item.Meta.Data.HasChildren || id == "favorite-root",
		})
	}
	return nodes, nil
}

func (d classicData) tuiTypeNodes() ([]assetTUITreeNode, error) {
	client := newLangAPIClient(d.api, d.lang)
	var page assetTUITreePage
	_, err := client.Call("GET", d.userPath("nodes/children-with-assets/category/tree/"), nil, &page,
		map[string]string{"include_assets": "false"})
	if err != nil {
		return nil, err
	}
	if len(page.Results) > classicTreeLimit {
		return nil, fmt.Errorf("type tree exceeds %d nodes", classicTreeLimit)
	}
	nodes := make([]assetTUITreeNode, 0, len(page.Results))
	for _, item := range page.Results {
		kind := strings.TrimSpace(item.Meta.Type)
		if item.ID != "ROOT" && kind != "category" && kind != "type" && kind != "platform" {
			continue
		}
		category, assetType := "", ""
		if item.ID != "ROOT" {
			category, assetType, err = normalizeAssetTUITypeFilter(
				kind, item.Meta.Category, item.Meta.AssetType,
			)
			if err != nil {
				return nil, err
			}
		}
		name, amount := classicTreeAmount(item.Name)
		node := assetTUITreeNode{
			identity: item.ID, id: item.ID, key: item.ID, parent: item.Parent,
			name: strings.TrimSpace(name), kind: kind, category: category,
			assetType: assetType, countLoaded: amount != nil,
		}
		if amount != nil {
			node.count = *amount
		}
		if item.ID == "ROOT" {
			node.kind = "root"
		}
		if node.identity == "" || node.name == "" {
			return nil, fmt.Errorf("type tree response is missing id or name")
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func normalizeAssetTUITypeFilter(kind, category, assetType string) (string, string, error) {
	category = strings.TrimSpace(category)
	assetType = strings.TrimSpace(assetType)
	switch kind {
	case "category":
		if category == "" {
			category = assetType
		}
		assetType = ""
		if category == "" {
			return "", "", fmt.Errorf("type tree response is missing category or type")
		}
	case "type":
		if category == "" || assetType == "" {
			return "", "", fmt.Errorf("type tree response is missing category or type")
		}
	}
	return category, assetType, nil
}
