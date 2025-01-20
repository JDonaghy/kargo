package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"

	//"os"
	"path/filepath"

	//"runtime"
	//"syscall"

	// "encoding/json"

	"bytes"
	"io"
	"log"
	"os/exec"

	//"time"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	//"io/ioutil"
	"github.com/awesome-gocui/gocui"
)

type Node struct {
	Name		string
	Description string
	Data		interface{}
	Selected	bool
	Expanded	bool
	Children	[]*Node
	LineNumber  int
	Color	    int
	OpenSymbol  string
	Symbol      string
}

type Tree struct {
	Root *Node
	charWidth int
}

type nodeInfo struct {
	namespace   string
	name		string
	nodeType	string
	execChannel chan bool
	logging	 bool
}

const podSymbol = "\ueba2"
const nsSymbol = "\uea8b"
const folderOpenSymbol = "\uf07c"
const folderSymbol = "\uf07b"

var kTree Tree
var clientset *kubernetes.Clientset
var logs []string
var selectedNode nodeInfo
var gui *gocui.Gui

func clearSelection(node *Node) {
	node.Selected = false
	for _, child := range node.Children {
		clearSelection(child);
	}
}

func selectNode(node *Node, line int) {
	node.Selected = false
	for _, child := range node.Children {
		selectNode(child, line);
	}
}

func (t *Tree) SelectNode(line int) {
	if t.Root == nil {
		return
	}
	clearSelection(t.Root)

	selectNode(t.Root, line)
}

// RenderAsText renders the tree as text
func (t *Tree) RenderAsText() string {
	if t.Root == nil {
		return "Empty tree"
	}
	resetTreeLineNumbers(t.Root)
	lineNum := 1
	return renderNode(t.Root, "", true, t.charWidth, &lineNum)
}

func resetTreeLineNumbers(node *Node) {
	node.LineNumber = -1
	for _, child := range node.Children {
		resetTreeLineNumbers(child);
	}
}

func tryNode(node *Node, line int) {
	if (node.LineNumber == line) {
		logMessage(fmt.Sprintf("match node %s %d", node.Name, line))
		changeNodeHandler(node)
	} else {
		for _, child := range node.Children {
			tryNode(child, line);
		}
	}
}

func (t *Tree) processLineNumberEvent(line int) {
	if t.Root == nil {
		return 
	}
	logMessage(fmt.Sprintf("ProcessLN %d", line))
	tryNode(t.Root, line)
}

func setBackgroundColorForLine(g *gocui.Gui, v *gocui.View, lineNumber int, color gocui.Attribute) error {
    lines := strings.Split(v.Buffer(), "\n")
    if lineNumber < 0 || lineNumber >= len(lines) {
        return fmt.Errorf("invalid line number")
    }

    v.Clear()
    v.SetWritePos(0, 0)

    for i, line := range lines {
        if i == lineNumber {
            v.SelBgColor = color
            fmt.Fprintln(v, line)
			v.SelBgColor = gocui.ColorDefault
        } else {
            fmt.Fprintln(v, line)
        }
    }

    return nil
}

func renderNode(node *Node, prefix string, isLast bool, charWidth int, line *int) string {
	var result strings.Builder
	resetCode := "\x1b[0m"
	backgroundColor := fmt.Sprintf("%s%dm", "\x1b[48;5;", 237)
	nodeColor := fmt.Sprintf("%s%dm", "\x1b[38;5;", node.Color)

	//Write the current node
	//result.WriteString(prefix)
	treePrefix := prefix
	if len(node.Children) > 0 {
		if (node.Expanded) {
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
	if charWidth - len(treePrefix) - len(node.Name) > 0 {
		spaces = strings.Repeat(" ", charWidth)
	} else {
		spaces = ""
	}

	if node.Selected {
		result.WriteString(fmt.Sprintf("%s%s%s%s%s%s\n", backgroundColor, treePrefix, nodeColor, node.Name, spaces, resetCode))
	} else {
		result.WriteString(fmt.Sprintf("%s%s%s%s%s\n", treePrefix, nodeColor, node.Name, spaces, resetCode))
	}
	*line++;
	if node.Expanded {

		// Recursively render child nodes
		for i, child := range node.Children {
			newPrefix := prefix
			//if isLast {
				newPrefix += "   "
			// } else {
			// 	newPrefix += "│  "
			// }
			nodeText := renderNode(child, newPrefix, i == len(node.Children)-1, charWidth, line)
			result.WriteString(nodeText)
		}
	}
	return result.String()
}


func drawTree(g *gocui.Gui) error {
	tv, err := g.View("tree")
	if err != nil {
		log.Fatal("failed to get textView", err)
	}
	tv.Clear()
	fmt.Fprintf(tv, kTree.RenderAsText())
	return nil
}

func clearView(g *gocui.Gui, view string) error {
	tv, err := g.View(view)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to get view %s", view), err)
	}
	tv.Clear()
	return nil
}

func drawString(g *gocui.Gui, view string, str string) error {
	dv, err := g.View(view)
	if err == nil {
		fmt.Fprintf(dv, "%s\n", str)
	}
	return nil
}

func drawStrings(g *gocui.Gui, view string, slice []string) error {
	dv, err := g.View(view)
	if err == nil {
		dv.Clear()
		for _, item := range slice {
			fmt.Fprintf(dv, "%s\n", item)
		}
	}
	return nil
}

func logMessage(msg string) {
	logs = append(logs, msg)
	drawStrings(gui, "log", logs[:])
}

func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	detailsX := (maxX * 15) / 100
	logX  := (maxX * 80) / 100
	kTree.charWidth = detailsX - 2
	if v, err := g.SetView("log", logX, 0, maxX-1, maxY-1, 0); err != nil {
		v.SelFgColor = gocui.ColorBlack
		v.SelBgColor = gocui.ColorGreen
		v.Autoscroll = true
		
		v.Title = " Log "
		if err != gocui.ErrUnknownView {
			return err
		}
		drawStrings(g, "log", logs[:])
	}
	if v, err := g.SetView("tree", 0, 0, detailsX - 1, maxY-1, 0); err != nil {
		v.SelFgColor = gocui.ColorBlack
		v.SelBgColor = gocui.ColorGreen
		//v.Autoscroll = true

		v.Title = " Tree "
		if err != gocui.ErrUnknownView {
			return err
		}
		drawTree(g)
	}
	if v, err := g.SetView("details", detailsX, 0, logX-1, maxY-1, 0); err != nil {
		v.SelFgColor = gocui.ColorBlack
		v.SelBgColor = gocui.ColorGreen
		v.Autoscroll = true

		v.Title = " Details "
		if err != gocui.ErrUnknownView {
			return err
		}
	}
	//logMessage(fmt.Sprintf("MX,MY: %d,%d", maxX, maxY))

	return nil
}


func DescribePod(namespace string, name string) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("kubectl describe pod %s -n %s", name, namespace))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	drawString(gui, "details", fmt.Sprintf("%s%s", stdout.Bytes(), stderr.Bytes()))
}

	// func writeToView(view *tview.TextView, writer tview.TextViewWriter, text string) {
	//	 fmt.Fprint(writer, text)
	//	 view.ScrollToEnd()
	// }

func LogPod(nodeinfo *nodeInfo) {
	nodeinfo.logging = true
	logMessage(fmt.Sprintf("Log pod %s", nodeinfo.name))
	go func(nodeInfo *nodeInfo) error {
		logOpts := coreV1.PodLogOptions{Follow: true}
		req := clientset.CoreV1().Pods(nodeinfo.namespace).GetLogs(nodeinfo.name, &logOpts)

		podLogs, err := req.Stream(context.Background())
		if err != nil {
			log.Fatal("failed to get pod logs: %v", err)
		}
		logMessage("0")
		defer podLogs.Close()
		logMessage("1")

		ch := make(chan string, 2)
		go func(ch chan string) {
			for i := 0; true; i++ {
				buf := make([]byte, 2000000)
				numBytes, err := podLogs.Read(buf)
				logMessage(fmt.Sprintln("Read", numBytes, " bytes", i))
				ch <- string(buf[:numBytes])
				if err == io.EOF {
					logMessage("EOF")
					break
				}
				if numBytes == 0 {
					if nodeInfo.logging == false {
						logMessage("l=false")
						break
					// } else {
					//	 logMessage("SLEEP")
					//	 time.Sleep(time.Second)
					//	 continue
					}
				}
				if err != nil {
					logMessage(fmt.Sprintln("Error reading output:", err))
					break
				}
			}
		}(ch)

		for {
			scanstr := <-ch
			logMessage("got bytes")
			gui.Update(func(g *gocui.Gui) error {
				drawString(gui, "details", scanstr)
				return nil
			})
			if len(nodeInfo.execChannel) > 0 {
				if <-nodeInfo.execChannel {
					logMessage("Got DIE!!!")
					nodeInfo.logging = false
					break
				}
			}
		}
		return nil
	}(nodeinfo)
}

	// func AddChildNode(node *tview.TreeNode, nodeText string, selectable bool, ref nodeInfo, color tcell.Color) *tview.TreeNode {
	//	 newNode := tview.NewTreeNode(nodeText).
	//		 SetSelectable(selectable).
	//		 SetReference(ref).
	//		 SetColor(color)
	//	 node.AddChild(newNode)
	//	 return newNode
	// }
	//

func changeNodeHandler(node *Node) {
	clearView(gui, "details")
	clearSelection(kTree.Root)

	nodeinfo := node.Data.(nodeInfo)
	namespace := nodeinfo.namespace
	children := node.Children
	node.Selected = true
	logMessage(fmt.Sprintf("CHG: type: %s, %s, %s, %s, %t", nodeinfo.nodeType, node.Name, selectedNode.name, selectedNode.namespace, selectedNode.logging))
	if selectedNode.logging {
		logMessage("DIE!")
		selectedNode.execChannel <- true
	} else {
		logMessage("NODIE!")
	}

	switch nodeinfo.nodeType {
	case "Pods":
		if len(children) == 0 {
			logs = append(logs, "Adding Pods")
			pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				log.Fatal(err)
			}
			for _, pod := range pods.Items {
				nInfo := nodeInfo{
					namespace:   namespace,
					name:		pod.Name,
					nodeType:	"pod",
					execChannel: make(chan bool, 3),
					logging:	 false,
				}
				addNode(node, pod.Name, "", 179, nInfo, podSymbol, podSymbol, false, false)
			}
		} else {
			node.Expanded = !node.Expanded
		}
	case "ConfigMaps":
		if len(children) == 0 {
			configmaps, err := clientset.CoreV1().ConfigMaps(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				log.Fatal(err)
			}
			for _, configmap := range configmaps.Items {
				addNode(node, configmap.Name, "", 27, nodeInfo{namespace: namespace, nodeType: "configmap"}, "", "", false, false) 
			}
		} else {
			node.Expanded = !node.Expanded
		}
	case "Services":
		if len(children) == 0 {
			services, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				log.Fatal(err)
			}
			for _, service := range services.Items {
				addNode(node, service.Name, "", 87, nodeInfo{namespace: namespace, nodeType: "service"}, "", "", false, false) 
			}
		} else {
			node.Expanded = !node.Expanded
		}
	case "pod":
		LogPod(&nodeinfo)
		// podInfo, err := clientset.CoreV1().Pods(ref.namespace).Get(context.Background(), node.GetText(), metav1.GetOptions{})
		// detailsJson, err := json.MarshalIndent(podInfo, "", "  ")
		// if err != nil {
		//	 panic(err)
		// }
	case "configmap":
	case "service":
	}
	selectedNode = nodeinfo

	// tv, err := gui.View("tree")
	// if err != nil {
	// 	log.Fatal("failed to get textView", err)
	// }

	// if err := setBackgroundColorForLine(gui, tv, 5, gocui.ColorYellow); err != nil {
 //    }
}

// func selectNodeHandler(node *tview.TreeNode) {
//	 reference := node.GetReference()
//	 if reference == nil {
//		 return
//	 }
//	 nodeinfo := reference.(nodeInfo)
//	 namespace := nodeinfo.namespace
//	 children := node.GetChildren()
//	 switch nodeinfo.nodeType {
//	 }
//
//	 selectedNode = nodeinfo
// }

func addNode(node *Node, name string, description string, color int, data interface{}, symbol string, openSymbol string, selected bool, expanded bool) *Node {

	childNode := Node{
		Name:		 name,
		Description: description,
		Data:		 data,
		Color:	     color,
		Selected:	 selected,
		Expanded:	 expanded,
		Symbol:      symbol,
		OpenSymbol:  openSymbol,
		Children:	 []*Node{},
	}
	if node != nil {
		node.Children = append(node.Children, &childNode)
	}
	return &childNode
}

func PopulateTree() Tree {
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(namespaces.Items[:], func(i, j int) bool {
		return namespaces.Items[i].Name < namespaces.Items[j].Name
	})
	
	root := addNode(nil, "Namespaces", "", 1, nodeInfo{namespace: "", nodeType: "Namespaces"}, "", "", false, true)

	for _, ns := range namespaces.Items {
		nsNode := addNode(root, ns.Name, "", 1, nodeInfo{namespace: ns.Name, nodeType: "Namespace"}, nsSymbol, nsSymbol, false, true)
		addNode(nsNode, "Pods", "", 2, nodeInfo{namespace: ns.Name, nodeType: "Pods"}, folderSymbol , folderOpenSymbol, false, true)
		addNode(nsNode, "ConfigMaps", "", 3, nodeInfo{namespace: ns.Name, nodeType: "ConfigMaps"}, folderSymbol , folderOpenSymbol, false, false)
		addNode(nsNode, "Services", "", 4, nodeInfo{namespace: ns.Name, nodeType: "Services"}, folderSymbol , folderOpenSymbol, false, false)
		logMessage("populated " + ns.Name)

	}

	// tree.SetSelectedFunc(selectNodeHandler)
	//tree.SetChangedFunc(changeNodeHandler)
	tree := Tree{Root: root}

	return tree
}


func init() {
	logs = []string{}
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

}
func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func mouseClick(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		_, cy := v.Cursor()

		kTree.processLineNumberEvent(cy + 1);
		//v.Clear()
		drawTree(g)
	}
	return nil
}

func scrollView(v *gocui.View, dy int) error {
	if v != nil {
		v.Autoscroll = false
		ox, oy := v.Origin()
		if err := v.SetOrigin(ox, oy+dy); err != nil {
			return err
		}
	}
	return nil
}

func ScrollUp(g *gocui.Gui, v *gocui.View) error {
	scrollView(v, -1)
	return nil
}

func ScrollDown(g *gocui.Gui, v *gocui.View) error {
	scrollView(v, 1)
	return nil
}

func initKeybindings() error {
	if err := gui.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		return err
	}
	if err := gui.SetKeybinding("", 'q', gocui.ModNone, quit); err != nil {
		return err
	}
	if err := gui.SetKeybinding("tree", gocui.MouseLeft, gocui.ModNone, mouseClick); err != nil {
		return err
	}
	if err := gui.SetKeybinding("tree", gocui.MouseWheelUp, gocui.ModNone, ScrollUp); err != nil {
		return err
	}
	if err := gui.SetKeybinding("tree", gocui.MouseWheelDown, gocui.ModNone, ScrollDown); err != nil {
		return err
	}
	if err := gui.SetKeybinding("tree", gocui.KeyArrowDown, gocui.ModNone, ScrollUp); err != nil {
		return err
	}
	if err := gui.SetKeybinding("tree", gocui.KeyArrowDown, gocui.ModNone, ScrollDown); err != nil {
		return err
	}
	return nil
}


func Run() error {
	var err error
	gui, err = gocui.NewGui(gocui.Output256, true)
	if err != nil {
		log.Fatal(err)
	}
	defer gui.Close()
	gui.Mouse = true
	kTree = PopulateTree()
	gui.SetManagerFunc(layout)

	initKeybindings()

	return gui.MainLoop()
}

func main() {
	err := Run();
	if err != nil {
		log.Panicln(err)
	}
}
