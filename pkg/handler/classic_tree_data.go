package handler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xlab/treeprint"

	"github.com/jumpserver-dev/sdk-go/model"
)

func ConstructNodeTree(assetNodes []model.Node) (treeprint.Tree, []model.Node) {
	model.SortNodesByKey(assetNodes)
	rootTree := treeprint.New()
	newNodes := make([]model.Node, 0, len(assetNodes))
	newNodes = constructDisplayTree(rootTree, convertToDisplayTrees(assetNodes), newNodes)
	return rootTree, newNodes
}

func constructNodeTreeRows(assetNodes []model.Node) ([]string, []model.Node) {
	model.SortNodesByKey(assetNodes)
	prefixes := make([]string, 0, len(assetNodes))
	ordered := make([]model.Node, 0, len(assetNodes))
	constructDisplayRows(convertToDisplayTrees(assetNodes), "", &prefixes, &ordered)
	return prefixes, ordered
}

func constructDisplayRows(nodes []*displayTree, prefix string, prefixes *[]string, ordered *[]model.Node) {
	for i, item := range nodes {
		last := i == len(nodes)-1
		edge := string(treeprint.EdgeTypeMid)
		childPrefix := prefix + string(treeprint.EdgeTypeLink) + strings.Repeat(" ", treeprint.IndentSize)
		if last {
			edge = string(treeprint.EdgeTypeEnd)
			childPrefix = prefix + strings.Repeat(" ", treeprint.IndentSize+1)
		}
		*prefixes = append(*prefixes, prefix+edge+" ")
		*ordered = append(*ordered, item.node)
		if len(item.subTrees) > 0 {
			sort.Sort(nodeTrees(item.subTrees))
			constructDisplayRows(item.subTrees, childPrefix, prefixes, ordered)
		}
	}
}

func constructDisplayTree(tree treeprint.Tree, rootNodes []*displayTree, newNodes []model.Node) []model.Node {
	for _, rootNode := range rootNodes {
		subTree := tree.AddBranch(fmt.Sprintf("%d.%s(%s)", len(newNodes)+1, rootNode.node.Name,
			strconv.Itoa(rootNode.node.AssetsAmount)))
		newNodes = append(newNodes, rootNode.node)
		if len(rootNode.subTrees) > 0 {
			sort.Sort(nodeTrees(rootNode.subTrees))
			newNodes = constructDisplayTree(subTree, rootNode.subTrees, newNodes)
		}
	}
	return newNodes
}

func convertToDisplayTrees(assetNodes []model.Node) []*displayTree {
	var rootNodeTrees []*displayTree
	nodeTreeMap := make(map[string]*displayTree)
	for i := range assetNodes {
		currentTree := displayTree{Key: assetNodes[i].Key, node: assetNodes[i]}
		separator := strings.LastIndex(assetNodes[i].Key, ":")
		if separator < 0 {
			rootNodeTrees = append(rootNodeTrees, &currentTree)
			nodeTreeMap[assetNodes[i].Key] = &currentTree
			continue
		}
		nodeTreeMap[assetNodes[i].Key] = &currentTree
		parentTree, ok := nodeTreeMap[assetNodes[i].Key[:separator]]
		if !ok {
			rootNodeTrees = append(rootNodeTrees, &currentTree)
			continue
		}
		parentTree.AddSubNode(&currentTree)
	}
	return rootNodeTrees
}

type displayTree struct {
	Key      string
	node     model.Node
	subTrees []*displayTree
}

func (t *displayTree) AddSubNode(sub *displayTree) { t.subTrees = append(t.subTrees, sub) }

type nodeTrees []*displayTree

func (l nodeTrees) Len() int           { return len(l) }
func (l nodeTrees) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l nodeTrees) Less(i, j int) bool { return l[i].node.Name < l[j].node.Name }
