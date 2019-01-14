package controller

import (
	"github.com/sirupsen/logrus"
	propsyv1 "gitlab.seznam.net/propsy/pkg/apis/propsy/v1"
	propsyclient "gitlab.seznam.net/propsy/pkg/client/clientset/versioned"
	ppsv1 "gitlab.seznam.net/propsy/pkg/client/clientset/versioned/typed/propsy/v1"
	informerext "gitlab.seznam.net/propsy/pkg/client/informers/externalversions"
	ppslisterv1 "gitlab.seznam.net/propsy/pkg/client/listers/propsy/v1"
	"gitlab.seznam.net/propsy/pkg/propsy"
	"k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"log"
	"reflect"
	"time"
)

const STATIC_WEIGHT_TODO = 1 // TODO make better if needed?
const CANARY_POSTFIX = "-canary"

// a single propsy controller that reads from kubernetes
type ProPsyController struct {
	kubeClient kubernetes.Interface
	locality   *propsy.Locality
	ppsCache   *propsy.ProPsyCache

	endpointGetter       corev1.EndpointsGetter
	endpointLister       listerv1.EndpointsLister
	endpointListerSynced cache.InformerSynced

	ppsGetter       ppsv1.ProPsyServicesGetter
	ppsLister       ppslisterv1.ProPsyServiceLister
	ppsListerSynced cache.InformerSynced
}

func NewProPsyController(endpointClient kubernetes.Interface, crdClient propsyclient.Interface, locality *propsy.Locality, ppsCache *propsy.ProPsyCache) (*ProPsyController, error) {
	sharedInformers := informers.NewSharedInformerFactory(endpointClient, 10*time.Second)
	endpointInformer := sharedInformers.Core().V1().Endpoints()

	var propsy ProPsyController

	if crdClient != nil {
		customInformers := informerext.NewSharedInformerFactory(crdClient, 10*time.Second)
		propsyInformer := customInformers.Propsy().V1().ProPsyServices()
		propsy = ProPsyController{
			kubeClient: endpointClient,
			locality:   locality,
			ppsCache:   ppsCache,

			endpointGetter:       endpointClient.CoreV1(),
			endpointLister:       endpointInformer.Lister(),
			endpointListerSynced: endpointInformer.Informer().HasSynced,

			ppsGetter:       crdClient.PropsyV1(),
			ppsLister:       propsyInformer.Lister(),
			ppsListerSynced: propsyInformer.Informer().HasSynced,
		}

		propsyInformer.Informer().AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					log.Printf("Add propsy: %+v", obj)
					propsy.PPSAdded(obj.(*propsyv1.ProPsyService))
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					propsy.PPSChanged(oldObj.(*propsyv1.ProPsyService), newObj.(*propsyv1.ProPsyService))
				},
				DeleteFunc: func(obj interface{}) {
					propsy.PPSRemoved(obj.(*propsyv1.ProPsyService))
				},
			},
		)
		customInformers.Start(nil)
	} else {
		propsy = ProPsyController{
			kubeClient: endpointClient,
			locality:   locality,
			ppsCache:   ppsCache,

			endpointGetter:       endpointClient.CoreV1(),
			endpointLister:       endpointInformer.Lister(),
			endpointListerSynced: endpointInformer.Informer().HasSynced,
		}
	}

	endpointInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				propsy.EndpointAdded(obj.(*v1.Endpoints))
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				propsy.EndpointChanged(oldObj.(*v1.Endpoints), newObj.(*v1.Endpoints))
			},
			DeleteFunc: func(obj interface{}) {
				propsy.EndpointRemoved(obj.(*v1.Endpoints))
			},
		},
	)

	sharedInformers.Start(nil)

	return &propsy, nil
}

func (C *ProPsyController) WaitForInitialSync(stop <-chan struct{}) {
	if !cache.WaitForCacheSync(stop, C.endpointListerSynced, C.ppsListerSynced) {
		log.Fatal("Error waiting to sync initial cache")
		return
	}

	log.Print("Finished syncing initial cache")
}

func (C *ProPsyController) EndpointAdded(endpoint *v1.Endpoints) {
	name := propsy.GenerateUniqueEndpointName(endpoint.Namespace, endpoint.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}

	for ie := range ecs {
		// it seems to be a tracked service. Feed in all the endpoints...
		for i := 0; i < len(endpoint.Subsets); i++ {
			for j := 0; j < len(endpoint.Subsets[i].Addresses); j++ {
				ecs[ie].AddEndpoint(C.locality, endpoint.Subsets[i].Addresses[j].IP, STATIC_WEIGHT_TODO)
			}
		}
	}

	for i := range nodes {
		nodes[i].Update()
	}
}

func (C *ProPsyController) EndpointRemoved(endpoint *v1.Endpoints) {
	name := propsy.GenerateUniqueEndpointName(endpoint.Namespace, endpoint.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	for i := range ecs {
		ecs[i].ClearLocality(C.locality)
	}

	for i := range nodes {
		nodes[i].Update()
	}
}

func (C *ProPsyController) EndpointChanged(old *v1.Endpoints, new *v1.Endpoints) {
	if reflect.DeepEqual(old, new) {
		return
	}

	name := propsy.GenerateUniqueEndpointName(old.Namespace, old.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)

	C.ppsCache.MutexEndpoints.Lock() // lock to prevent other localities resetting before we fill in ourselves to avoid sending config with empty data
	defer C.ppsCache.MutexEndpoints.Unlock()

	for i := range ecs {
		ecs[i].ClearLocality(C.locality) // clear the existing from this locality. do NOT update tracked nodes until we feed the new ones in!!
		C.EndpointAdded(new)             // feed in new ones
	}

	for j := range nodes {
		nodes[j].Update()
	}
}

func (C *ProPsyController) PPSAdded(pps *propsyv1.ProPsyService) {
	// TODO support disabled flag
	var nodes []*propsy.NodeConfig
	for i := 0; i < len(pps.Spec.Nodes); i++ {
		node := C.ppsCache.GetOrCreateNode(pps.Spec.Nodes[i])
		nodes = append(nodes, node)
		logrus.Infof("Node: %p, %s", node, node.NodeName)
	}

	uniqueName := propsy.GenerateUniqueEndpointName(pps.Namespace, pps.Name)
	uniqueNameCanary := propsy.GenerateUniqueEndpointName(pps.Namespace, pps.Name+CANARY_POSTFIX)
	endpointName := propsy.GenerateUniqueEndpointName(pps.Namespace, pps.Spec.Service)
	endpointNameCanary := propsy.GenerateUniqueEndpointName(pps.Namespace, pps.Spec.CanaryService)

	endpointConfig := propsy.EndpointConfig{
		Name:        endpointName,
		ServicePort: pps.Spec.ServicePort,
		Endpoints:   map[*propsy.Locality][]*propsy.Endpoint{},
	}

	endpointConfigCanary := propsy.EndpointConfig{
		Name:        endpointNameCanary,
		ServicePort: pps.Spec.ServicePort,
		Endpoints:   map[*propsy.Locality][]*propsy.Endpoint{},
	}

	clusterConfig := propsy.ClusterConfig{
		Weight: pps.Spec.Percent,

		Name:           uniqueName,
		ConnectTimeout: pps.Spec.Timeout,
		EndpointConfig: &endpointConfig,
	}

	clusterConfigCanary := propsy.ClusterConfig{
		Weight:         pps.Spec.CanaryPercent,
		Name:           uniqueNameCanary,
		ConnectTimeout: pps.Spec.Timeout,
		EndpointConfig: &endpointConfigCanary,
	}

	clusterConfigs := []*propsy.ClusterConfig{&clusterConfig}
	if pps.Spec.CanaryService != "" {
		clusterConfigs = append(clusterConfigs, &clusterConfigCanary)
	}

	routeConfig := propsy.RouteConfig{
		Name:     uniqueName,
		Clusters: clusterConfigs,
	}

	listenerConfig := &propsy.ListenerConfig{
		Name:   uniqueName,
		Listen: pps.Spec.Listen,
		VirtualHosts: []*propsy.VirtualHost{{
			Name:    uniqueName,
			Domains: []string{"*"}, // TODO ?
			Routes:  []*propsy.RouteConfig{&routeConfig},
		}},
	}

	C.ppsCache.RegisterEndpointSet(&endpointConfig, nodes)
	if pps.Spec.CanaryService != "" {
		C.ppsCache.RegisterEndpointSet(&endpointConfigCanary, nodes)
	}

	for node := range nodes {
		nodes[node].AddListener(listenerConfig)
	}

	endpoints, err := C.endpointGetter.Endpoints(pps.Namespace).Get(pps.Spec.Service, v12.GetOptions{})
	if err == nil {
		C.EndpointAdded(endpoints)
	}

	if pps.Spec.CanaryService != "" {
		endpointsCanary, err := C.endpointGetter.Endpoints(pps.Namespace).Get(pps.Spec.CanaryService, v12.GetOptions{})
		if err == nil {
			C.EndpointAdded(endpointsCanary)
		}
	}

	for node := range nodes {
		logrus.Infof("Dispatching update to %s", nodes[node].NodeName)
		nodes[node].Update()
	}
	logrus.Infof("cache content: %+v", C.ppsCache)
	C.ppsCache.DumpNodes()
}

func (C *ProPsyController) PPSRemoved(pps *propsyv1.ProPsyService) {
	for i := range pps.Spec.Nodes {
		node := C.ppsCache.GetOrCreateNode(pps.Spec.Nodes[i])
		node.Free()
	}

	// TODO do we have some way to remove nodes from envoy apiserver?
}

func (C *ProPsyController) PPSChanged(old *propsyv1.ProPsyService, new *propsyv1.ProPsyService) {
	// TODO
	logrus.Printf("Content of cache: %+v", C.ppsCache)
	if reflect.DeepEqual(old, new) {
		return
	}

	if !reflect.DeepEqual(old.Spec.Nodes, new.Spec.Nodes) {
		// TODO rework nodes.. bleh :P code below will crash on node change until this gets implemented
	}

	var newNodes []*propsy.NodeConfig
	for i := 0; i < len(new.Spec.Nodes); i++ {
		newNodes = append(newNodes, C.ppsCache.GetOrCreateNode(new.Spec.Nodes[i]))
	}

	uniqueName := propsy.GenerateUniqueEndpointName(new.Namespace, new.Name)
	uniqueNameEndpointsNew := propsy.GenerateUniqueEndpointName(new.Namespace, new.Spec.Service)
	uniqueNameEndpointsOld := propsy.GenerateUniqueEndpointName(new.Namespace, old.Spec.Service)
	uniqueNameEndpointsCanaryNew := propsy.GenerateUniqueEndpointName(new.Namespace, new.Spec.CanaryService+CANARY_POSTFIX)
	uniqueNameEndpointsCanaryOld := propsy.GenerateUniqueEndpointName(new.Namespace, old.Spec.CanaryService+CANARY_POSTFIX)

	if old.Spec.ServicePort != new.Spec.ServicePort {
		for i := range newNodes {
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).FindCluster(uniqueNameEndpointsNew).EndpointConfig.ServicePort = new.Spec.ServicePort
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).FindCluster(uniqueNameEndpointsCanaryNew).EndpointConfig.ServicePort = new.Spec.ServicePort
		}

		// regenerate endpoint list
		endpoints, err := C.endpointGetter.Endpoints(new.Namespace).Get(new.Spec.Service, v12.GetOptions{})
		if err != nil {
			copied := endpoints.DeepCopy()
			copied.Annotations["foobar"] = "old" // force change
			C.EndpointChanged(copied, endpoints)
		}

		if new.Spec.CanaryService != "" {
			endpointsCanary, err := C.endpointGetter.Endpoints(new.Namespace).Get(new.Spec.CanaryService, v12.GetOptions{})
			if err == nil {
				copied := endpointsCanary.DeepCopy()
				copied.Annotations["foobar"] = "old" // force change
				C.EndpointChanged(copied, endpointsCanary)
			}
		}
	}

	if old.Spec.Listen != new.Spec.Listen {
		for i := range newNodes {
			newNodes[i].FindListener(uniqueName).Listen = new.Spec.Listen
		}
	}

	if old.Spec.Disabled != new.Spec.Disabled {

	}

	if old.Spec.Percent != new.Spec.Percent {
		for i := range newNodes {
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).FindCluster(uniqueNameEndpointsNew).Weight = new.Spec.Percent
		}
	}

	if old.Spec.Service != new.Spec.Service {
		// disconnect old services
		endpointConfig := propsy.EndpointConfig{
			Name:        uniqueNameEndpointsNew,
			ServicePort: new.Spec.ServicePort,
		}

		for i := range newNodes {
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).RemoveCluster(uniqueNameEndpointsOld)
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).AddCluster(&propsy.ClusterConfig{
				Name:           uniqueNameEndpointsNew,
				Weight:         new.Spec.Percent,
				ConnectTimeout: new.Spec.Timeout,
				EndpointConfig: &endpointConfig,
			})

		}

		C.ppsCache.RegisterEndpointSet(&endpointConfig, newNodes)
		C.ppsCache.RemoveEndpointSet(uniqueNameEndpointsOld, newNodes)
	}

	if old.Spec.CanaryService != new.Spec.CanaryService {
		endpointConfigCanary := propsy.EndpointConfig{
			Name:        uniqueNameEndpointsCanaryNew,
			ServicePort: new.Spec.ServicePort,
		}

		for i := range newNodes {
			if old.Spec.CanaryService != "" {
				newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).RemoveCluster(uniqueNameEndpointsCanaryOld)
			}
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).AddCluster(&propsy.ClusterConfig{
				Name:           uniqueNameEndpointsCanaryNew,
				Weight:         new.Spec.CanaryPercent,
				ConnectTimeout: new.Spec.Timeout,
				EndpointConfig: &endpointConfigCanary,
			})
		}

		C.ppsCache.RegisterEndpointSet(&endpointConfigCanary, newNodes)
		if old.Spec.CanaryService != "" {
			C.ppsCache.RemoveEndpointSet(uniqueNameEndpointsCanaryOld, newNodes)
		}
	}

	if old.Spec.CanaryPercent != new.Spec.CanaryPercent {
		for i := range newNodes {
			newNodes[i].FindListener(uniqueName).FindVHost(uniqueName).FindRoute(uniqueName).FindCluster(uniqueNameEndpointsCanaryNew).Weight = new.Spec.CanaryPercent
		}
	}

	// todo dispatch update

}
