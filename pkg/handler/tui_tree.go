package handler

import (
	"strconv"
	"strings"

	"github.com/rivo/tview"
)

// The synthetic container is not an API level, even when displayed as All assets.
func (h *terminalUI) treeExpansion() (expanded, deeper bool) {
	root := h.tree.GetRoot()
	if root != nil {
		root.Walk(func(n, parent *tview.TreeNode) bool {
			if n != root && n.IsExpanded() {
				expanded = true
				deeper = deeper || parent != root
			}
			return n.IsExpanded()
		})
	}
	return
}

func (h *terminalUI) toggleTreeExpansion() {
	if expanded, _ := h.treeExpansion(); expanded {
		h.collapseTree()
	} else {
		h.expandFirstTreeLevel()
	}
}

func (h *terminalUI) collapseTree() {
	if _, deeper := h.treeExpansion(); deeper {
		h.expandFirstTreeLevel()
	} else {
		h.expandTree(false)
	}
}

func (h *terminalUI) expandFirstTreeLevel() {
	root := h.tree.GetRoot()
	if root == nil {
		return
	}
	h.treeCollapsed = false
	root.Walk(func(n, parent *tview.TreeNode) bool {
		ref := n.GetReference().(*tuiNodeRef)
		first := parent == root && !ref.more
		n.SetExpanded(n == root || first)
		ref.defaultExpand = first && !ref.loaded
		h.refreshNodeLabel(n)
		return true
	})
	h.selectFirstTreeNode()
}

func (h *terminalUI) selectFirstTreeNode() {
	node := h.tree.GetRoot()
	if node != nil && h.scope.Mode == 0 {
		children := node.GetChildren()
		node = nil
		if len(children) > 0 {
			node = children[0]
		}
	}
	h.tree.SetCurrentNode(node)
}

// Reveal the list's scope without selecting a different node or reloading assets.
func (h *terminalUI) focusAssetNode() {
	h.focusArea(h.tree)
	root := h.tree.GetRoot()
	if root == nil {
		return
	}
	var reveal func(*tview.TreeNode) bool
	reveal = func(node *tview.TreeNode) bool {
		if ref, ok := node.GetReference().(*tuiNodeRef); ok && !ref.more && ref.scope == h.scope && (node != root || h.scope.Mode != 0) {
			h.tree.SetCurrentNode(node)
			return true
		}
		for _, child := range node.GetChildren() {
			if reveal(child) {
				node.SetExpanded(true)
				h.refreshNodeLabel(node)
				return true
			}
		}
		return false
	}
	reveal(root)
}

// tview scrolls the viewport without moving its cursor, then pulls an off-screen
// cursor back into view on the next redraw (clock tick, metrics or page load).
// Keep the navigation cursor at the nearest visible row while the wheel moves;
// this does not select a new asset scope or initiate any connection.
func (h *terminalUI) keepTreeSelectionInView(step int) {
	root := h.tree.GetRoot()
	if root == nil {
		return
	}
	var rows []*tview.TreeNode
	selected := -1
	root.Walk(func(n, _ *tview.TreeNode) bool {
		if n == root && h.scope.Mode == 0 {
			return true
		}
		if n == h.tree.GetCurrentNode() {
			selected = len(rows)
		}
		rows = append(rows, n)
		return n.IsExpanded()
	})
	_, _, _, height := h.tree.GetInnerRect()
	if height < 1 || len(rows) == 0 {
		return
	}
	first := max(0, min(h.tree.GetScrollOffset()+step, len(rows)-height))
	last := min(len(rows)-1, first+height-1)
	if selected < first {
		h.tree.SetCurrentNode(rows[first])
	} else if selected > last {
		h.tree.SetCurrentNode(rows[last])
	}
}

// Lina's type tree takes category/type counts from the trailing label amount.
func typeTreeAmount(label string) (string, *int) {
	text := strings.TrimSpace(label)
	start := strings.LastIndex(text, "(")
	if start >= 0 && strings.HasSuffix(text, ")") {
		if count, err := strconv.Atoi(text[start+1 : len(text)-1]); err == nil && count >= 0 {
			return strings.TrimSpace(text[:start]), &count
		}
	}
	return label, nil
}

func (r *tuiNodeRef) metricID() string {
	if r.more || r.scope.Mode == 1 {
		return ""
	}
	if r.scope.Mode == 2 {
		if r.scope.FolderID == "" {
			return "favorite-root"
		}
		return r.scope.FolderID
	}
	if r.scope.Key == "ungrouped" {
		return "ungrouped"
	}
	return r.scope.NodeID
}

// Run after drawing: tview updates the scroll offset during Draw, for both
// keyboard navigation and mouse scrolling. Match Lina's 100-row metric batches
// and prefetch the next node page within 20 rows of its loaded subtree's end.
func (h *terminalUI) advanceTree() {
	root := h.tree.GetRoot()
	if root == nil || h.sidebarHidden || h.modal || h.activeSession >= 0 {
		return
	}
	w, ht := h.screen.Size()
	if w < 72 || ht < 20 {
		return
	}
	first := h.tree.GetScrollOffset()
	_, _, _, height := h.tree.GetInnerRect()
	countEnd := ((first+tuiTreeBatchSize/2)/tuiTreeBatchSize + 1) * tuiTreeBatchSize
	var counts []*tview.TreeNode
	var next, parent, expand *tview.TreeNode
	row := 0
	root.Walk(func(n, p *tview.TreeNode) bool {
		if n == root && h.scope.Mode == 0 {
			return true
		}
		ref := n.GetReference().(*tuiNodeRef)
		if row < countEnd && ref.metricID() != "" && !ref.countRequested {
			counts = append(counts, n)
		}
		if next == nil && ref.more && !ref.autoRequested && row >= first && row <= first+height+20 {
			next, parent = n, p
		}
		if expand == nil && ref.defaultExpand && !ref.loaded && n.IsExpanded() && row >= first && row <= first+height+20 {
			expand = n
		}
		row++
		return n.IsExpanded()
	})
	if !h.countLoading && len(counts) > 0 {
		h.loadNodeCounts(counts[:min(tuiTreeBatchSize, len(counts))])
	}
	if !h.treeLoading && expand != nil {
		h.loadTree(expand, expand.GetReference().(*tuiNodeRef).scope, "")
	} else if !h.treeLoading && next != nil && parent != nil {
		ref := next.GetReference().(*tuiNodeRef)
		// An error leaves an explicit, keyboard-accessible retry entry. Do not
		// retry on every redraw while the service is unavailable.
		ref.autoRequested = true
		h.loadTree(parent, ref.scope, ref.cursor)
	}
}

// Counts have an independent worker and never block tree or asset rendering.
// Expanding a branch does not fetch the descendants of its collapsed children.
func (h *terminalUI) loadNodeCounts(nodes []*tview.TreeNode) {
	ids := make([]string, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		ref := n.GetReference().(*tuiNodeRef)
		id := ref.metricID()
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
		ref.countRequested = true
	}
	h.countLoading = true
	generation, data, scope := h.viewGeneration, h.data, h.scope
	h.queue(h.countJobs, func() {
		counts, err := data.nodeCounts(scope.Org.ID, scope.Mode, ids)
		h.update(func() {
			if generation != h.viewGeneration {
				return
			}
			h.countLoading = false
			for _, n := range nodes {
				ref := n.GetReference().(*tuiNodeRef)
				if count, ok := counts[ref.metricID()]; err == nil && ok {
					ref.count = &count
				}
				h.refreshNodeLabel(n)
			}
		})
	})
}
