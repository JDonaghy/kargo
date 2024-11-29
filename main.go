package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"

	//"strings"
	//"time"

	//"k8s.io/apimachinery/pkg/api/errors"
	//apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	//"io/ioutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var clientset *kubernetes.Clientset

func AddChildNode(node *tview.TreeNode, nodeText string, selectable bool, ref string, color tcell.Color) *tview.TreeNode {
	newNode := tview.NewTreeNode(nodeText).
		SetSelectable(selectable).
		SetReference(ref).
		SetColor(color)
	node.AddChild(newNode)
	return newNode
}

func selectNodeHandler(node *tview.TreeNode) {
	reference := node.GetReference()
	if reference == nil {
		return
	}
	namespace := reference.(string)
	children := node.GetChildren()
	if len(children) == 0 {
		switch node.GetText() {
		case "Pods":
			pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				panic(err.Error())
			}
			for _, pod := range pods.Items {
				AddChildNode(node, pod.Name, true, "", tcell.ColorGreen)
			}

		case "ConfigMaps":
			configmaps, err := clientset.CoreV1().ConfigMaps(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				panic(err.Error())
			}
			for _, configmap := range configmaps.Items {
				AddChildNode(node, configmap.Name, true, "", tcell.ColorGreen)
			}
		}

	} else {
		// Collapse if visible, expand if collapsed.
		node.SetExpanded(!node.IsExpanded())
	}
}

func PopulateTree() *tview.TreeView {
	treeRoot := "namespace"
	root := tview.NewTreeNode(treeRoot).
		SetColor(tcell.ColorYellow)
	tree := tview.NewTreeView().
		SetRoot(root).
		SetCurrentNode(root)

	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		panic(err.Error())
	}

	for _, ns := range namespaces.Items {
		node := AddChildNode(root, ns.Name, true, fmt.Sprintf("Namespace: %s", ns.Name), tcell.ColorGreen) 
		AddChildNode(node, "Pods", true, ns.Name, tcell.ColorYellow)
		AddChildNode(node, "ConfigMaps", true, ns.Name, tcell.ColorYellow)
	}

	tree.SetSelectedFunc(selectNodeHandler)
	return tree
}

func init() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
}

func main() {
	tree := PopulateTree()

	if err := tview.NewApplication().SetRoot(tree, true).Run(); err != nil {
		panic(err)
	}
}
