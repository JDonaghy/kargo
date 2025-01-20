package gocuitree

type Tree struct {
	Root         *Node
	CharWidth    int
	SelectedNode *Node
}

func NewTree(rootNodeName string, color int, data interface{},
	symbol string, openSymbol string,
	selected bool, expanded bool) (tree *Tree) {
	node := newNode(rootNodeName, color, data, openSymbol, symbol, selected, expanded)
	return &Tree{
		Root:         node,
		SelectedNode: nil,
	}
}

func (t *Tree) ProcessLineEvent(line int, eventHandler func(node *Node)) {
	if t.Root == nil {
		return
	}
	t.Root.tryProcessNode(line, eventHandler)
}

func (t *Tree) RenderAsText(charWidth int) string {
	if t == nil {
		return ""
	}
	t.Root.resetTreeLineNumbers()
	lineNum := 1
	return t.Root.renderNode("", true, charWidth, &lineNum)
}

func (t *Tree) ClearSelection() {
	t.Root.clearSelection()
}

