package format

import (
	"sort"

	"github.com/cespare/goclj"
	"github.com/cespare/goclj/parse"
)

// A Transform is some tree transformation that can be applied after parsing and
// before printing. Some Transforms may use some heuristics that cause them to
// change code semantics in certain cases; these are clearly indicated and none
// of these are enabled by default.
type Transform int

const (
	// TransformSortImportRequire sorts :import and :require declarations
	// in ns blocks.
	TransformSortImportRequire Transform = iota
	// TransformRemoveTrailingNewlines removes extra newlines following
	// sequence-like forms, so that parentheses are written on the same
	// line. For example,
	//   (foo bar
	//    )
	// becomes
	//   (foo bar)
	TransformRemoveTrailingNewlines
	// TransformFixDefnArglistNewline moves the arg vector of defns to the
	// same line, if appropriate:
	//   (defn foo
	//     [x] ...)
	// becomes
	//   (defn foo [x]
	//     ...)
	// if there's no newline after the arg list.
	TransformFixDefnArglistNewline
	// TransformFixDefmethodDispatchValNewline moves the dispatch-val of a
	// defmethod to the same line, so that
	//   (defmethod foo
	//     :bar
	//     [x] ...)
	// becomes
	//   (defmethod foo :bar
	//     [x] ...)
	TransformFixDefmethodDispatchValNewline
	// TransformRemoveExtraBlankLines consolidates consecutive blank lines
	// into a single blank line.
	TransformRemoveExtraBlankLines
)

var DefaultTransforms = map[Transform]bool{
	TransformSortImportRequire:              true,
	TransformRemoveTrailingNewlines:         true,
	TransformFixDefnArglistNewline:          true,
	TransformFixDefmethodDispatchValNewline: true,
	TransformRemoveExtraBlankLines:          true,
}

func applyTransforms(t *parse.Tree, transforms map[Transform]bool) {
	for _, root := range t.Roots {
		if transforms[TransformSortImportRequire] &&
			goclj.FnFormSymbol(root, "ns") {
			sortNS(root)
		}
		if transforms[TransformRemoveTrailingNewlines] {
			removeTrailingNewlines(root)
		}
		if transforms[TransformFixDefnArglistNewline] &&
			goclj.FnFormSymbol(root, "defn") {
			fixDefnArglist(root)
		}
		if transforms[TransformFixDefmethodDispatchValNewline] &&
			goclj.FnFormSymbol(root, "defmethod") {
			fixDefmethodDispatchVal(root)
		}
		if transforms[TransformRemoveExtraBlankLines] {
			removeExtraBlankLinesRecursive(root)
		}
	}
	if transforms[TransformRemoveExtraBlankLines] {
		t.Roots = removeExtraBlankLines(t.Roots)
	}
}

func sortNS(ns parse.Node) {
	for _, n := range ns.Children()[1:] {
		if goclj.FnFormKeyword(n, ":require", ":import") {
			sortImportRequire(n.(*parse.ListNode))
		}
	}
}

func sortImportRequire(n *parse.ListNode) {
	var (
		nodes             = n.Children()
		sorted            = make(importRequireList, 0, len(nodes)/2)
		lineComments      []*parse.CommentNode
		afterSemanticNode = false
	)
	for _, node := range nodes[1:] {
		switch node := node.(type) {
		case *parse.CommentNode:
			if afterSemanticNode {
				sorted[len(sorted)-1].CommentBeside = node
			} else {
				lineComments = append(lineComments, node)
			}
		case *parse.NewlineNode:
			afterSemanticNode = false
		default:
			ir := &importRequire{
				CommentsAbove: lineComments,
				Node:          node,
			}
			sorted = append(sorted, ir)
			lineComments = nil
			afterSemanticNode = true
		}
	}
	sort.Stable(sorted)
	newNodes := []parse.Node{nodes[0]}
	for _, ir := range sorted {
		for _, cn := range ir.CommentsAbove {
			newNodes = append(newNodes, cn, &parse.NewlineNode{})
		}
		newNodes = append(newNodes, ir.Node)
		if ir.CommentBeside != nil {
			newNodes = append(newNodes, ir.CommentBeside)
		}
		newNodes = append(newNodes, &parse.NewlineNode{})
	}
	// unattached comments at the bottom
	for _, cn := range lineComments {
		newNodes = append(newNodes, cn, &parse.NewlineNode{})
	}
	// drop trailing newline
	if len(newNodes) >= 2 && !goclj.Comment(newNodes[len(newNodes)-2]) {
		newNodes = newNodes[:len(newNodes)-1]
	}
	n.SetChildren(newNodes)
}

func removeTrailingNewlines(n parse.Node) {
	nodes := n.Children()
	if len(nodes) == 0 {
		return
	}
	switch n.(type) {
	case *parse.ListNode, *parse.MapNode, *parse.VectorNode, *parse.FnLiteralNode, *parse.SetNode:
		for ; len(nodes) > 0; nodes = nodes[:len(nodes)-1] {
			if len(nodes) >= 2 && goclj.Comment(nodes[len(nodes)-2]) {
				break
			}
			if !goclj.Newline(nodes[len(nodes)-1]) {
				break
			}
		}
		n.SetChildren(nodes)
	}
	for _, node := range nodes {
		removeTrailingNewlines(node)
	}
}

func fixDefnArglist(defn parse.Node) {
	nodes := defn.Children()
	if len(nodes) < 5 {
		return
	}
	if !goclj.Newline(nodes[2]) || goclj.Newline(nodes[4]) {
		return
	}
	if !goclj.Vector(nodes[3]) {
		return
	}
	// Move the newline to be after the arglist.
	nodes[2], nodes[3] = nodes[3], nodes[2]
	defn.SetChildren(nodes)
}

func fixDefmethodDispatchVal(defmethod parse.Node) {
	nodes := defmethod.Children()
	if len(nodes) < 5 {
		return
	}
	if !goclj.Newline(nodes[2]) {
		return
	}
	if !goclj.Keyword(nodes[3]) {
		return
	}
	// Move the dispatch-val up to the same line.
	// Insert a newline after if there wasn't one already.
	if goclj.Newline(nodes[4]) {
		nodes = append(nodes[:2], nodes[3:]...)
	} else {
		nodes[2], nodes[3] = nodes[3], nodes[2]
	}
	defmethod.SetChildren(nodes)
}

func removeExtraBlankLinesRecursive(n parse.Node) {
	nodes := n.Children()
	if len(nodes) == 0 {
		return
	}
	if len(nodes) > 2 {
		nodes = removeExtraBlankLines(nodes)
		n.SetChildren(nodes)
	}
	for _, node := range nodes {
		removeExtraBlankLinesRecursive(node)
	}
}

func removeExtraBlankLines(nodes []parse.Node) []parse.Node {
	newNodes := make([]parse.Node, 0, len(nodes))
	newlines := 0
	for _, node := range nodes {
		if goclj.Newline(node) {
			newlines++
		} else {
			newlines = 0
		}
		if newlines <= 2 {
			newNodes = append(newNodes, node)
		}
	}
	return newNodes
}

// An importRequire is an import/require with associated comment nodes.
type importRequire struct {
	CommentsAbove []*parse.CommentNode
	CommentBeside *parse.CommentNode
	Node          parse.Node
}

type importRequireList []*importRequire

func (l importRequireList) Len() int      { return len(l) }
func (l importRequireList) Swap(i, j int) { l[i], l[j] = l[j], l[i] }

func (l importRequireList) Less(i, j int) bool {
	// Some case are nonsenical; don't particularly care how those are sorted.
	n1, n2 := l[i].Node, l[j].Node
	if s1, ok := n1.(*parse.SymbolNode); ok {
		if s2, ok := n2.(*parse.SymbolNode); ok {
			return s1.Val < s2.Val
		}
		if goclj.Vector(n2) {
			return true // a < [b]
		}
		return true // a < 3
	}
	if listOrVector(n1) {
		if listOrVector(n2) {
			children1, children2 := n1.Children(), n2.Children()
			if len(children1) == 0 {
				return true // [] < [a]
			}
			if len(children2) == 0 {
				return false // [a] >= []
			}
			if p1, ok := children1[0].(*parse.SymbolNode); ok {
				if p2, ok := children2[0].(*parse.SymbolNode); ok {
					return p1.Val < p2.Val // [a] < [b]
				}
				return true // [a] < [3]
			}
			return false // [3] >= [a]
		}
		return false // [a] >= 3
	}
	return false // 3 >= 3
}

func listOrVector(node parse.Node) bool {
	switch node.(type) {
	case *parse.ListNode, *parse.VectorNode:
		return true
	}
	return false
}
