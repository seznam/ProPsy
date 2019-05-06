package main

import (
	"errors"
	"flag"
	"github.com/seznam/ProPsy/pkg/controller"
	"github.com/seznam/ProPsy/pkg/propsy"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"net"
	"os"
	"strings"

	clientset "github.com/seznam/ProPsy/pkg/client/clientset/versioned"
)

type ConnectedCluster struct {
	KubeconfigPath string // empty for in-cluster
	Zone           string
}

//var localities map[string]*propsy.Locality

type EndpointClusters []ConnectedCluster
type ConfigClusters []ConnectedCluster

func (i EndpointClusters) String() string {
	return "wat"
}

func (i ConfigClusters) String() string {
	return "wat"
}

func (i EndpointClusters) Set(flag string) error {
	parts := strings.Split(flag, ":")
	if len(parts) < 2 || len(parts) > 2 {
		return errors.New("not enough or too many parts in connected clusters")
	}

	endpointClusters = append(endpointClusters, ConnectedCluster{parts[0], parts[1]})

	return nil
}

func (i ConfigClusters) Set(flag string) error {
	parts := strings.Split(flag, ":")
	if len(parts) < 2 || len(parts) > 2 {
		return errors.New("not enough or too many parts in connected clusters")
	}

	configClusters = append(configClusters, ConnectedCluster{parts[0], parts[1]})

	return nil
}

var endpointClusters EndpointClusters
var configClusters ConfigClusters
var debugMode bool
var listenConfig string

func init() {
	flag.Var(&endpointClusters, "endpointcluster", "Kubernetes endpoint cluster map kubeconfigPath:zone")
	flag.Var(&configClusters, "configcluster", "Kubernetes config cluster map kubeconfigPath:zone")
	flag.BoolVar(&debugMode, "debug", false, "Enable debug output")
	flag.StringVar(&listenConfig, "listen", ":8888", "IP:Port to listen on")

	//localities = map[string]*propsy.Locality{}
}

func main() {
	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.Parse()
	if debugMode {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.InfoLevel)
	}

	logrus.SetOutput(os.Stdout)

	propsy.InitGRPCServer()

	if len(endpointClusters) == 0 || len(configClusters) == 0 {
		logrus.Fatal("There are no endpoint or config clusters defined. Exiting!")
	}

	cache := propsy.NewProPsyCache()

	lis, _ := net.Listen("tcp", listenConfig)
	go func() {
		if err := propsy.GetGRPCServer().Serve(lis); err != nil {
			logrus.Fatalf("Error starting a grpc server... %s", err.Error())
		}
	}()

	logrus.Info("Starting all the endpoint controllers")

	var ecs []*controller.EndpointController

	for i := 0; i < len(endpointClusters); i++ {
		logrus.Infof("Locality: %s", endpointClusters[i].Zone)
		cfg, err := clientcmd.BuildConfigFromFlags("", endpointClusters[i].KubeconfigPath)
		if err != nil {
			logrus.Fatalf("Error building kubeconfig: %s", err.Error())
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			logrus.Fatalf("Error building kubernetes clientset: %s", err.Error())
		}

		locality := propsy.Locality{Zone: endpointClusters[i].Zone}
		//localities[endpointClusters[i].Zone] = &locality

		ec, _ := controller.NewEndpointController(kubeClient, &locality, cache)
		ec.WaitForInitialSync(nil)
		ecs = append(ecs, ec)
	}

	logrus.Info("Starting all the propsy controllers")

	for i := 0; i < len(configClusters); i++ {
		logrus.Infof("Locality: %s", configClusters[i].Zone)
		cfg, err := clientcmd.BuildConfigFromFlags("", configClusters[i].KubeconfigPath)
		if err != nil {
			logrus.Fatalf("Error building kubeconfig: %s", err.Error())
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			logrus.Fatalf("Error building kubernetes clientset: %s", err.Error())
		}

		crdClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			logrus.Fatalf("Error building kubernetes crd clientset: %s", err.Error())
		}

		locality := propsy.Locality{Zone: configClusters[i].Zone}
		//localities[configClusters[i].Zone] = &locality

		ppsc, _ := controller.NewProPsyController(kubeClient, crdClient, &locality, cache, ecs)
		ppsc.WaitForInitialSync(nil)
	}


	cache.ProcessQueueOnce()

	// todo flip ready flag, the best we can do
	cache.Run()
}
