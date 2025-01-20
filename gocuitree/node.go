package gocuitree

import (
	"fmt"
	"strings"
)

type Node struct {
	Name       string
	Data       interface{}
	Selected   bool
	Expanded   bool
	LineNumber int
	Color      int
	Symbol     string
	OpenSymbol string
	Children   []*Node
}

func newNode(name string, color int, data interface{}, symbol string,
	openSymbol string, selected bool, expanded bool) *Node {
	return &Node{
		Name:       name,
		Data:       data,
		Color:      color,
		Selected:   selected,
		Expanded:   expanded,
		Symbol:     symbol,
		OpenSymbol: openSymbol,
		Children:   []*Node{},
	}
}

func (node *Node) renderNode(
	prefix string, isLast bool, charWidth int, line *int) string {
	var result strings.Builder
	resetCode := "\x1b[0m"
	backgroundColor := fmt.Sprintf("%s%dm", "\x1b[48;5;", 237)
	nodeColor := fmt.Sprintf("%s%dm", "\x1b[38;5;", node.Color)

	//Write the current node
	//result.WriteString(prefix)
	treePrefix := prefix
	if len(node.Children) > 0 {
		if node.Expanded {
			treePrefix += "─ "
		} else {
			treePrefix += "+ "
		}
	}
	if node.Expanded {
		treePrefix += node.OpenSymbol
	} else {
		treePrefix += node.Symbol
	}
	treePrefix += " "

	node.LineNumber = *line

	var spaces string
	if charWidth-len(treePrefix)-len(node.Name) > 0 {
		spaces = strings.Repeat(" ", charWidth)
	} else {
		spaces = ""
	}

	if node.Selected {
		result.WriteString(fmt.Sprintf("%s%s%s%s%s%s\n", backgroundColor,
			treePrefix, nodeColor, node.Name, spaces, resetCode))
	} else {
		result.WriteString(fmt.Sprintf("%s%s%s%s%s\n", treePrefix,
			nodeColor, node.Name, spaces, resetCode))
	}
	*line++
	if node.Expanded {

		// Recursively render child nodes
		for i, child := range node.Children {
			newPrefix := prefix
			newPrefix += "   "
			nodeText := child.renderNode(newPrefix, i == len(node.Children)-1, charWidth, line)
			result.WriteString(nodeText)
		}
	}
	return result.String()
}

func (node *Node) resetTreeLineNumbers() {
	node.LineNumber = -1
	for _, child := range node.Children {
		child.resetTreeLineNumbers()
	}
}

func (node *Node) clearSelection() {
	node.Selected = false
	for _, child := range node.Children {
		child.clearSelection()
	}
}

func (node *Node) tryProcessNode(line int, nodeProcessor func(node *Node)) {
	if node.LineNumber == line {
		nodeProcessor(node)
	} else {
		for _, child := range node.Children {
			child.tryProcessNode(line, nodeProcessor)
		}
	}
}

func (node *Node) AddChildNode(name string, color int, data interface{},
	symbol string, openSymbol string, selected bool, expanded bool) *Node {
	childNode := newNode(name, color, data, openSymbol, symbol, selected, expanded)
	node.Children = append(node.Children, childNode)
	return childNode
}
