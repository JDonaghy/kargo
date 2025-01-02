package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	//"os"
	"path/filepath"

	//"runtime"
	//"syscall"

	// "encoding/json"

	"bytes"
	"io"
	"log"
	"os/exec"
	"os/signal"
	"syscall"

	"time"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	//"io/ioutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type nodeInfo struct {
	namespace   string
	name        string
	nodeType    string
	execChannel chan bool
	logging     bool
}

var clientset *kubernetes.Clientset
var detailsInfo *tview.TextView
var logInfo *tview.TextView
var selectedNode nodeInfo
var logViewWriter tview.TextViewWriter
var podViewWriter tview.TextViewWriter

func DescribePod(namespace string, name string) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("kubectl describe pod %s -n %s", name, namespace))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	detailsInfo.SetText(fmt.Sprintf("%s%s", stdout.Bytes(), stderr.Bytes()))
}

func writeToView(view *tview.TextView, writer tview.TextViewWriter, text string) {
	fmt.Fprint(writer, text)
	view.ScrollToEnd()
}

func LogPod(nodeinfo *nodeInfo, name string) {
	nodeinfo.logging = true
	go func(nodeinfo *nodeInfo) error {
		logOpts := coreV1.PodLogOptions{Follow: true}
		req := clientset.CoreV1().Pods(nodeinfo.namespace).GetLogs(name, &logOpts)
		podLogs, err := req.Stream(context.Background())
		if err != nil {
			log.Fatalf("Failed to get pod logs: %v", err)
		}
		fmt.Fprintln(logViewWriter, "0")
		defer podLogs.Close()
		fmt.Fprintln(logViewWriter, "2")
		for {
			if len(nodeinfo.execChannel) > 0 {
				if <-nodeinfo.execChannel {
					fmt.Fprintln(logViewWriter, "Got DIE!!!")
					nodeinfo.logging = false
					logOpts.Follow = false
					break
				}
			}
		}
		fmt.Fprintln(logViewWriter, "1")

		ch := make(chan string, 2)
		go func(ch chan string) {
			for i := 0; true; i++ {
				buf := make([]byte, 200000)
				numBytes, err := podLogs.Read(buf)
				writeToView(logInfo, logViewWriter, fmt.Sprintln("Read", numBytes, " bytes", i))
				ch <- string(buf[:numBytes])
				if err == io.EOF {
					writeToView(logInfo, logViewWriter, "EOF")
					break
				}
				if numBytes == 0 {
					if nodeinfo.logging == false {
						break
					} else {
						writeToView(logInfo, logViewWriter, "SLEEP")
						time.Sleep(time.Second)
						continue
					}
				}
				if err != nil {
					writeToView(logInfo, logViewWriter, fmt.Sprintln("Error reading output:", err))
					break
				}
			}
		}(ch)

		for {
			if len(ch) > 0 {
				scanstr := <-ch
				writeToView(detailsInfo, podViewWriter, scanstr)
			}
			if len(nodeinfo.execChannel) > 0 {
				if <-nodeinfo.execChannel {
					fmt.Fprintln(logViewWriter, "Got DIE!!!")
					nodeinfo.logging = false
					break
				}
			} else {
				time.Sleep(time.Second)
			}
		}
		return nil
	}(nodeinfo)
	fmt.Fprintln(logViewWriter, "EXIT Logpod")
}

func AddChildNode(node *tview.TreeNode, nodeText string, selectable bool, ref nodeInfo, color tcell.Color) *tview.TreeNode {
	newNode := tview.NewTreeNode(nodeText).
		SetSelectable(selectable).
		SetReference(ref).
		SetColor(color)
	node.AddChild(newNode)
	return newNode
}

func changeNodeHandler(node *tview.TreeNode) {
	reference := node.GetReference()
	detailsInfo.SetText("")
	if reference == nil {
		return
	}
	nodeRef := reference.(nodeInfo)
	namespace := nodeRef.namespace
	children := node.GetChildren()
	fmt.Fprintln(logViewWriter, fmt.Sprintf("CHG: type: %s, %s, %s, %s, %t", nodeRef.nodeType, node.GetText(), selectedNode.name, selectedNode.namespace, selectedNode.logging))
	if selectedNode.logging {
		fmt.Fprintln(logViewWriter, "DIE!")
		selectedNode.execChannel <- true
	}

	switch nodeRef.nodeType {
	case "Pods":
		if len(children) == 0 {
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
				AddChildNode(node, pod.Name, true, nInfo, tcell.ColorGreen)
			}
		} else {
			node.SetExpanded(!node.IsExpanded())
		}
	case "ConfigMaps":
		if len(children) == 0 {
			configmaps, err := clientset.CoreV1().ConfigMaps(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				log.Fatal(err)
			}
			for _, configmap := range configmaps.Items {
				AddChildNode(node, configmap.Name, true, nodeInfo{namespace: namespace, nodeType: "configmap"}, tcell.ColorGreen)
			}
		} else {
			node.SetExpanded(!node.IsExpanded())
		}
	case "Services":
		if len(children) == 0 {
			services, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})

			if err != nil {
				log.Fatal(err)
			}
			for _, service := range services.Items {
				AddChildNode(node, service.Name, true, nodeInfo{namespace: namespace, nodeType: "service"}, tcell.ColorGreen)
			}
		} else {
			node.SetExpanded(!node.IsExpanded())
		}
	case "pod":
		LogPod(&nodeRef, node.GetText())
		// podInfo, err := clientset.CoreV1().Pods(ref.namespace).Get(context.Background(), node.GetText(), metav1.GetOptions{})
		// detailsJson, err := json.MarshalIndent(podInfo, "", "  ")
		// if err != nil {
		// 	panic(err)
		// }
	case "configmap":
	case "service":
	}
	selectedNode = nodeRef
}

// func selectNodeHandler(node *tview.TreeNode) {
// 	reference := node.GetReference()
// 	if reference == nil {
// 		return
// 	}
// 	nodeRef := reference.(nodeInfo)
// 	namespace := nodeRef.namespace
// 	children := node.GetChildren()
// 	switch nodeRef.nodeType {
// 	}
//
// 	selectedNode = nodeRef
// }

func PopulateTree() *tview.TreeView {
	treeRoot := "namespace"
	root := tview.NewTreeNode(treeRoot).
		SetColor(tcell.ColorBlue)
	tree := tview.NewTreeView().
		SetRoot(root).
		SetCurrentNode(root)

	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(namespaces.Items[:], func(i, j int) bool {
		return namespaces.Items[i].Name < namespaces.Items[j].Name
	})

	for _, ns := range namespaces.Items {
		node := AddChildNode(root, ns.Name, true, nodeInfo{namespace: ns.Name, nodeType: "Namespace"}, tcell.ColorBlue)
		AddChildNode(node, "Pods", true, nodeInfo{namespace: ns.Name, nodeType: "Pods"}, tcell.ColorGreen)
		AddChildNode(node, "ConfigMaps", true, nodeInfo{namespace: ns.Name, nodeType: "ConfigMaps"}, tcell.ColorGreen)
		AddChildNode(node, "Services", true, nodeInfo{namespace: ns.Name, nodeType: "Services"}, tcell.ColorGreen)
	}

	// tree.SetSelectedFunc(selectNodeHandler)
	tree.SetChangedFunc(changeNodeHandler)
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
		log.Fatal(err)
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	detailsInfo = tview.NewTextView()
	detailsInfo.SetScrollable(true)
	podViewWriter = detailsInfo.BatchWriter()
	defer podViewWriter.Close()
	podViewWriter.Clear()
	logInfo = tview.NewTextView()
	logInfo.SetScrollable(true)
	logViewWriter = logInfo.BatchWriter()
	defer logViewWriter.Close()
	logViewWriter.Clear()
}

func main() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Println("Received signal:", sig)
		if selectedNode.logging {
			fmt.Println("Cleaning up...")
			selectedNode.execChannel <- true
			time.Sleep(time.Second) // TODO: Find a better way
		}
		fmt.Println("Shutting down...")
		os.Exit(0)
	}()

	select {
	case <-sigChan:
	default:
		tree := PopulateTree()
		grid := tview.NewGrid().
			SetRows(0).
			SetColumns(-1, -3, -1).
			SetBorders(true).
			AddItem(tree, 0, 0, 1, 1, 0, 0, true).
			AddItem(detailsInfo, 0, 1, 1, 1, 0, 0, true).
			AddItem(logInfo, 0, 2, 1, 1, 0, 0, true)

		app := tview.NewApplication().SetRoot(grid, true)
		app.EnableMouse(true)
		err := app.Run()
		if err != nil {
			panic(err)
		}
	}
}
