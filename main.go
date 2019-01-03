package main

import (
	"errors"
	"flag"
	"github.com/sirupsen/logrus"
	"gitlab.seznam.net/propsy/pkg/controller"
	"gitlab.seznam.net/propsy/pkg/propsy"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"log"
	"net"
	"strings"

	clientset "gitlab.seznam.net/propsy/pkg/client/clientset/versioned"
)

type ConnectedCluster struct {
	KubeconfigPath string // empty for in-cluster
	Zone           string
}

var localities map[string]*propsy.Locality

type ConnectedClusters []ConnectedCluster

func (i ConnectedClusters) String() string {
	return "wat"
}

func (i ConnectedClusters) Set(flag string) error {
	parts := strings.Split(flag, ":")
	if len(parts) < 2 || len(parts) > 2 {
		return errors.New("not enough or too many parts in connected clusters")
	}

	connectedClusters = append(connectedClusters, ConnectedCluster{parts[0], parts[1]})

	return nil
}

var connectedClusters ConnectedClusters
var localZone string

func init() {
	flag.StringVar(&localZone, "zone", "", "Local zone")
	flag.Var(&connectedClusters, "cluster", "Kubernetes cluster map kubeconfigPath:zone")

	localities = map[string]*propsy.Locality{}
}

func main() {

	flag.Parse()
	if len(connectedClusters) == 0 {
		log.Fatal("There are zero clusters defined. Exitting!")
	}

	cache := propsy.NewProPsyCache()

	for i := 0; i < len(connectedClusters); i++ {
		cfg, err := clientcmd.BuildConfigFromFlags("", connectedClusters[i].KubeconfigPath)
		if err != nil {
			log.Fatalf("Error building kubeconfig: %s", err.Error())
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
		}

		crdClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			log.Fatalf("Error building kubernetes crd clientset: %s", err.Error())
		}

		locality := propsy.Locality{Zone: connectedClusters[i].Zone}
		localities[connectedClusters[i].Zone] = &locality

		controller, _ := controller.NewProPsyController(kubeClient, crdClient, &locality, cache)
		controller.WaitForInitialSync(nil)
	}

	log.Print("Almost ready, starting a controller loop to generate configs")

	cache.ProcessQueueOnce()

	lis, _ := net.Listen("tcp", ":8888")

	go func() {
		if err := propsy.GetGRPCServer().Serve(lis); err != nil {
			logrus.Fatalf("Error starting a grpc server... %s", err.Error())
		}
	}()

	// todo flip ready flag, the best we can do
	cache.Run()

	//informerFactory := informers.NewSharedInformerFactory(exampleClient, time.Second * 30)
	/*stuff, err := exampleClient.PropsyV1().ProPsyServices("").List(v1.ListOptions{})
		if err != nil {
			panic(err)
		}

		fmt.Printf("found: %+v", stuff)
	/*
		stuff2, _ := kubeClient.AppsV1().Deployments("").List(v1.ListOptions{})

		fmt.Printf("blabla: %+v", stuff2)
	*/
}
