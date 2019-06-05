package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/seznam/ProPsy/pkg/controller"
	"github.com/seznam/ProPsy/pkg/propsy"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	clientset "github.com/seznam/ProPsy/pkg/client/clientset/versioned"
)

type EndpointCluster struct {
	KubeconfigPath string // empty for in-cluster
	Zone           string
	Priority       int
}

type ConfigCluster struct {
	KubeconfigPath string // empty for in-cluster
	Zone           string
}

//var localities map[string]*propsy.Locality

type EndpointClusters []EndpointCluster
type ConfigClusters []ConfigCluster

func (i EndpointClusters) String() string {
	return "wat"
}

func (i ConfigClusters) String() string {
	return "wat"
}

func (i EndpointClusters) Set(flag string) error {
	parts := strings.Split(flag, ":")
	if len(parts) < 3 || len(parts) > 3 {
		return errors.New("not enough or too many parts in connected clusters")
	}

	priority, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil {
		return errors.New(fmt.Sprintf("wrong priority: %s", err.Error()))
	}
	for i := range endpointClusters {
		if endpointClusters[i].Priority == int(priority) {
			return errors.New(fmt.Sprintf("this priority has been assigned already to %s", endpointClusters[i].KubeconfigPath))
		}
	}

	endpointClusters = append(endpointClusters, EndpointCluster{parts[0], parts[1], int(priority)})

	return nil
}

func (i ConfigClusters) Set(flag string) error {
	parts := strings.Split(flag, ":")
	if len(parts) < 2 || len(parts) > 2 {
		return errors.New("not enough or too many parts in connected clusters")
	}

	configClusters = append(configClusters, ConfigCluster{parts[0], parts[1]})

	return nil
}

var endpointClusters EndpointClusters
var configClusters ConfigClusters
var debugMode bool
var listenConfig string
var listenHealth string

var healthServer *propsy.HealthServer

func init() {
	flag.Var(&endpointClusters, "endpointcluster", "Kubernetes endpoint cluster map kubeconfigPath:zone:priority")
	flag.Var(&configClusters, "configcluster", "Kubernetes config cluster map kubeconfigPath:zone")
	flag.BoolVar(&debugMode, "debug", false, "Enable debug output")
	flag.StringVar(&listenConfig, "listen", ":8888", "IP:Port to listen on")
	flag.StringVar(&listenHealth, "listenhealth", ":9999", "IP:Port to listen on for health endpoints")

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

	healthServer = propsy.NewHealthServer(listenHealth)
	healthServer.SetHealthy(true)
	healthServer.Start()

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
		logrus.Infof("Priority: %d", endpointClusters[i].Priority)
		cfg, err := clientcmd.BuildConfigFromFlags("", endpointClusters[i].KubeconfigPath)
		if err != nil {
			logrus.Fatalf("Error building kubeconfig: %s", err.Error())
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			logrus.Fatalf("Error building kubernetes clientset: %s", err.Error())
		}

		//localities[endpointClusters[i].Zone] = &locality

		ec, _ := controller.NewEndpointController(kubeClient, endpointClusters[i].Priority, endpointClusters[i].Zone, cache)
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

	// there's no easy way to discover that the initial sync has happened
	// so let's wait for 3 seconds of no changes before we flip the readiness flag
	for time.Now().Sub(cache.LatestPPSAdded) < time.Second*3 {
		time.Sleep(time.Second)
		logrus.Debug("waiting for initial PPS to be added")
	}
	logrus.Info("Enabling readiness flag")
	healthServer.SetReady(true)

	cache.Run()
}
