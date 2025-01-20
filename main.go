package main

import (
	"context"
	"flag"
	"fmt"
	"sort"

	"path/filepath"

	"bytes"
	"io"
	"log"
	"os/exec"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/awesome-gocui/gocui"

	"kargo/texttree"
)

type nodeInfo struct {
	namespace   string
	name        string
	nodeType    string
	execChannel chan bool
	logging     bool
}

const (
	podSymbol        = "\ueba2"
	nsSymbol         = "\uea8b"
	folderOpenSymbol = "\uf07c"
	folderSymbol     = "\uf07b"
)

var (
	kTree     *texttree.Tree
	clientset *kubernetes.Clientset
	logs      []string
	gui       *gocui.Gui
)

func drawTree(g *gocui.Gui) error {
	tv, err := g.View("tree")
	if err != nil {
		log.Fatal("failed to get textView", err)
	}
	tv.Clear()
	fmt.Fprintf(tv, kTree.RenderAsText(kTree.CharWidth))
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
	logX := (maxX * 80) / 100
	kTree.CharWidth = detailsX - 2
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
	if v, err := g.SetView("tree", 0, 0, detailsX-1, maxY-1, 0); err != nil {
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
		defer podLogs.Close()

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
						break
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
			gui.Update(func(g *gocui.Gui) error {
				drawString(gui, "details", scanstr)
				return nil
			})
			if len(nodeInfo.execChannel) > 0 {
				if <-nodeInfo.execChannel {
					logMessage("Got log termination signal")
					nodeInfo.logging = false
					break
				}
			}
		}
		return nil
	}(nodeinfo)
}

func changeNodeHandler(node *texttree.Node) {
	clearView(gui, "details")
	kTree.ClearSelection()

	nodeinfo := node.Data.(nodeInfo)
	node.Selected = true
	namespace := nodeinfo.namespace
	children := node.Children

	if kTree.SelectedNode != nil {
		selectedNodeinfo := kTree.SelectedNode.Data.(nodeInfo)
		if selectedNodeinfo.logging {
			logMessage("Stop logging")
			selectedNodeinfo.execChannel <- true
		} else {
			logMessage("No logging to stop")
		}
	} else {
		logMessage(fmt.Sprintf("changeNodeHandler: type: %s, %s, no previous node selected", nodeinfo.nodeType, node.Name))
	}

	switch nodeinfo.nodeType {
	case "Namespaces":
		node.Expanded = !node.Expanded
	case "Namespace":
		node.Expanded = !node.Expanded
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
					name:        pod.Name,
					nodeType:    "pod",
					execChannel: make(chan bool, 3),
					logging:     false,
				}
				node.AddChildNode(pod.Name, 179, nInfo, podSymbol, podSymbol, false, false)
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
				node.AddChildNode(
					configmap.Name, 27, nodeInfo{namespace: namespace, nodeType: "configmap"}, 
					"", "", false, false)
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
				node.AddChildNode(
					service.Name, 87, nodeInfo{namespace: namespace, nodeType: "service"}, 
					"", "", false, false)
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
	kTree.SelectedNode = node
}

func PopulateTree() *texttree.Tree {
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(namespaces.Items[:], func(i, j int) bool {
		return namespaces.Items[i].Name < namespaces.Items[j].Name
	})

	newTree := texttree.NewTree("Namespaces", 1, nodeInfo{namespace: "", nodeType: "Namespaces"}, "", "", false, true)

	for _, ns := range namespaces.Items {
		nsNode := newTree.Root.AddChildNode(
			ns.Name, 1, nodeInfo{namespace: ns.Name, nodeType: "Namespace"}, 
			nsSymbol, nsSymbol, false, false)
		nsNode.AddChildNode(
			"Pods", 2, nodeInfo{namespace: ns.Name, nodeType: "Pods"}, 
			folderSymbol, folderOpenSymbol, false, true)
		nsNode.AddChildNode(
			"ConfigMaps", 3, nodeInfo{namespace: ns.Name, nodeType: "ConfigMaps"}, 
			folderSymbol, folderOpenSymbol, false, false)
		nsNode.AddChildNode(
			"Services", 4, nodeInfo{namespace: ns.Name, nodeType: "Services"}, 
			folderSymbol, folderOpenSymbol, false, false)
		logMessage("Populated " + ns.Name)
	}

	return newTree
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

		kTree.ProcessLineEvent(cy+1, changeNodeHandler)
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
	err := Run()
	if err != nil {
		log.Panicln(err)
	}
}
