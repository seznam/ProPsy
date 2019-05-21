package controller

import (
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"log"
	"reflect"
	"time"

	"k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/seznam/ProPsy/pkg/propsy"
)

const STATIC_WEIGHT_TODO = 1 // TODO make better if needed?

type EndpointController struct {
	endpointGetter       corev1.EndpointsGetter
	endpointLister       listerv1.EndpointsLister
	endpointListerSynced cache.InformerSynced

	ppsCache *propsy.ProPsyCache

	Priority int
	Zone     string
}

func NewEndpointController(endpointClient kubernetes.Interface, priority int, zone string, ppsCache *propsy.ProPsyCache) (*EndpointController, error) {
	sharedInformers := informers.NewSharedInformerFactory(endpointClient, 10*time.Minute)
	endpointInformer := sharedInformers.Core().V1().Endpoints()

	ec := EndpointController{
		endpointGetter:       endpointClient.CoreV1(),
		endpointLister:       endpointInformer.Lister(),
		endpointListerSynced: endpointInformer.Informer().HasSynced,

		ppsCache: ppsCache,

		Priority: priority,
		Zone:     zone,
	}

	endpointInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				ec.EndpointAdded(obj.(*v1.Endpoints))
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				ec.EndpointChanged(oldObj.(*v1.Endpoints), newObj.(*v1.Endpoints))
			},
			DeleteFunc: func(obj interface{}) {
				ec.EndpointRemoved(obj.(*v1.Endpoints))
			},
		},
	)

	sharedInformers.Start(nil)

	return &ec, nil
}

func (C *EndpointController) WaitForInitialSync(stop <-chan struct{}) {
	logrus.Debug("Waiting for sync...")
	if !cache.WaitForCacheSync(stop, C.endpointListerSynced) {
		log.Fatal("Error waiting to sync initial cache")
		return
	}

	logrus.Print("Finished syncing initial cache")
}

func (C *EndpointController) EndpointAdded(endpoint *v1.Endpoints) {
	name := propsy.GenerateUniqueEndpointName(C.Priority, endpoint.Namespace, endpoint.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}

	ecs.Endpoints = []*propsy.Endpoint{} // init so we know it exists

	// it seems to be a tracked service. Feed in all the endpoints...
	for i := 0; i < len(endpoint.Subsets); i++ {
		for j := 0; j < len(endpoint.Subsets[i].Addresses); j++ {
			ecs.AddEndpoint(endpoint.Subsets[i].Addresses[j].IP, STATIC_WEIGHT_TODO, true)
		}
		for j := 0; j < len(endpoint.Subsets[i].NotReadyAddresses); j++ {
			ecs.AddEndpoint(endpoint.Subsets[i].NotReadyAddresses[j].IP, STATIC_WEIGHT_TODO, false)
		}
	}

	for i := range nodes {
		nodes[i].Update()
	}
}

func (C *EndpointController) EndpointRemoved(endpoint *v1.Endpoints) {
	name := propsy.GenerateUniqueEndpointName(C.Priority, endpoint.Namespace, endpoint.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}
	ecs.Endpoints = nil

	for i := range nodes {
		nodes[i].Update()
	}
}

func (C *EndpointController) EndpointChanged(old *v1.Endpoints, new *v1.Endpoints) {
	if reflect.DeepEqual(old, new) {
		return
	}

	name := propsy.GenerateUniqueEndpointName(C.Priority, old.Namespace, old.Name)
	ecs, _ := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}

	C.ppsCache.MutexEndpoints.Lock() // lock to prevent other localities resetting before we fill in ourselves to avoid sending config with empty data
	defer C.ppsCache.MutexEndpoints.Unlock()

	ecs.Endpoints = []*propsy.Endpoint{} // clear the existing from this locality. do NOT update tracked nodes until we feed the new ones in!!
	C.EndpointAdded(new)                 // feed in new ones

	logrus.Debugf("New endpoint count for %s: %d", new.Name, len(new.Subsets[0].Addresses))
	for i := range new.Subsets[0].Addresses {
		logrus.Debugf("endpoint: %s", new.Subsets[0].Addresses[i].IP)
	}

	for i := range ecs.Endpoints {
		logrus.Debugf("ecs endpoint: %s %t", ecs.Endpoints[i].Host, ecs.Endpoints[i].Healthy)
	}
}

func (C *EndpointController) ResyncEndpoints(namespace, service, canary string) {
	endpoints, err := C.endpointGetter.Endpoints(namespace).Get(service, v12.GetOptions{})
	if err == nil {
		C.EndpointAdded(endpoints)
	} else {
		if errors.IsNotFound(err) {
			name := propsy.GenerateUniqueEndpointName(C.Priority, namespace, service)
			ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
			ecs.Endpoints = nil
			for i := range nodes {
				nodes[i].Update()
			}
		}
		logrus.Debugf("no such endpoint, err: %s", err.Error())
	}

	if canary != "" {
		endpointsCanary, err := C.endpointGetter.Endpoints(namespace).Get(canary, v12.GetOptions{})
		if err == nil {
			C.EndpointAdded(endpointsCanary)
		}
	}
}
