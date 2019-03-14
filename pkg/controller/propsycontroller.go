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

	secretGetter       corev1.SecretsGetter
	secretLister       listerv1.SecretLister
	secretListerSynced cache.InformerSynced

	ppsGetter       ppsv1.ProPsyServicesGetter
	ppsLister       ppslisterv1.ProPsyServiceLister
	ppsListerSynced cache.InformerSynced
}

func NewProPsyController(endpointClient kubernetes.Interface, crdClient propsyclient.Interface, locality *propsy.Locality, ppsCache *propsy.ProPsyCache) (*ProPsyController, error) {
	sharedInformers := informers.NewSharedInformerFactory(endpointClient, 10*time.Second)
	endpointInformer := sharedInformers.Core().V1().Endpoints()
	secretInformer := sharedInformers.Core().V1().Secrets()

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

			secretGetter:       endpointClient.CoreV1(),
			secretLister:       secretInformer.Lister(),
			secretListerSynced: secretInformer.Informer().HasSynced,

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

			secretGetter:       endpointClient.CoreV1(),
			secretLister:       secretInformer.Lister(),
			secretListerSynced: secretInformer.Informer().HasSynced,
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

	secretInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				propsy.SecretAdded(obj.(*v1.Secret))
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				propsy.SecretChanged(oldObj.(*v1.Secret), newObj.(*v1.Secret))
			},
			DeleteFunc: func(obj interface{}) {
				propsy.SecretRemoved(obj.(*v1.Secret))
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

func (C *ProPsyController) SecretAdded(secret *v1.Secret) {
	logrus.Debugf("Secret added: %s:%s", secret.Namespace, secret.Name)
	C.ppsCache.UpdateTLS(secret.Namespace, secret.Name, secret.Data["tls.crt"], secret.Data["tls.key"])
}

func (C *ProPsyController) SecretRemoved(secret *v1.Secret) {
	logrus.Debugf("Secret removed: %s:%s", secret.Namespace, secret.Name)
	C.ppsCache.UpdateTLS(secret.Namespace, secret.Name, []byte{}, []byte{})
}

func (C *ProPsyController) SecretChanged(old, new *v1.Secret) {
	if reflect.DeepEqual(old, new) {
		return
	}

	logrus.Debugf("Secret changed: %s:%s", new.Namespace, new.Name)
	C.SecretAdded(new) // just overwrite with the new one
}

func (C *ProPsyController) ResyncTLS(namespace, name string) {
	secret, err := C.secretGetter.Secrets(namespace).Get(name, v12.GetOptions{})
	if err == nil {
		C.SecretAdded(secret)
	}
}

func (C *ProPsyController) EndpointAdded(endpoint *v1.Endpoints) {
	name := propsy.GenerateUniqueEndpointName(C.locality, endpoint.Namespace, endpoint.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}

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

func (C *ProPsyController) EndpointRemoved(endpoint *v1.Endpoints) {
	name := propsy.GenerateUniqueEndpointName(C.locality, endpoint.Namespace, endpoint.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}
	ecs.Endpoints = []*propsy.Endpoint{}

	for i := range nodes {
		nodes[i].Update()
	}
}

func (C *ProPsyController) EndpointChanged(old *v1.Endpoints, new *v1.Endpoints) {
	if reflect.DeepEqual(old, new) {
		return
	}

	name := propsy.GenerateUniqueEndpointName(C.locality, old.Namespace, old.Name)
	ecs, nodes := C.ppsCache.GetEndpointSetByEndpoint(name)
	if ecs == nil {
		return
	}

	C.ppsCache.MutexEndpoints.Lock() // lock to prevent other localities resetting before we fill in ourselves to avoid sending config with empty data
	defer C.ppsCache.MutexEndpoints.Unlock()

	ecs.Endpoints = []*propsy.Endpoint{} // clear the existing from this locality. do NOT update tracked nodes until we feed the new ones in!!
	C.EndpointAdded(new)                 // feed in new ones

	for j := range nodes {
		nodes[j].Update()
	}
}

func (C *ProPsyController) NewCluster(pps *propsyv1.ProPsyService, isCanary bool) *propsy.ClusterConfig {
	var endpointName string
	var percent int
	if isCanary {
		endpointName = propsy.GenerateUniqueEndpointName(C.locality, pps.Namespace, pps.Spec.CanaryService)
		percent = pps.Spec.CanaryPercent
	} else {
		endpointName = propsy.GenerateUniqueEndpointName(C.locality, pps.Namespace, pps.Spec.Service)
		percent = pps.Spec.Percent
	}

	endpointConfig := propsy.EndpointConfig{
		Name:        endpointName,
		ServicePort: pps.Spec.ServicePort,
		Endpoints:   []*propsy.Endpoint{},
		Locality:    C.locality,
	}

	return &propsy.ClusterConfig{
		ConnectTimeout: pps.Spec.Timeout,
		Name:           endpointName,
		Weight:         percent,
		EndpointConfig: &endpointConfig,
		IsCanary:       isCanary,
	}
}

func (C *ProPsyController) NewRouteConfig(pps *propsyv1.ProPsyService) *propsy.RouteConfig {
	clusterConfig := C.NewCluster(pps, false)
	clusterConfigCanary := C.NewCluster(pps, true)

	clusterConfigs := []*propsy.ClusterConfig{clusterConfig}
	if pps.Spec.CanaryService != "" {
		clusterConfigs = append(clusterConfigs, clusterConfigCanary)
	}

	routeName, path := propsy.GenerateRouteName(pps.Spec.PathPrefix)

	return &propsy.RouteConfig{
		Name:          routeName,
		Clusters:      clusterConfigs,
		PathPrefix:    path,
		PrefixRewrite: pps.Spec.PrefixRewrite,
	}
}

func GetProxyType(typeInPps string) propsy.ProxyType {
	switch typeInPps {
	case "HTTP":
		return propsy.HTTP
	case "TCP":
		return propsy.TCP
	case "":
		return propsy.HTTP
	default:
		logrus.Error("Unknown propsy type " + typeInPps)
		return -1
	}
}

func (C *ProPsyController) NewListenerConfig(pps *propsyv1.ProPsyService) *propsy.ListenerConfig {
	propsyType := GetProxyType(pps.Spec.Type)
	if propsyType == -1 {
		return nil
	}

	domains := []string{"*"} // TODO ?
	vhostName := propsy.GenerateVHostName(domains)
	listenerName := propsy.GenerateListenerName(pps.Spec.Listen, propsyType)

	var tlsData *propsy.TlsData = nil
	if pps.Spec.TLSCertificateSecret != "" {
		tlsData = C.ppsCache.GetOrCreateTLS(pps.Namespace, pps.Spec.TLSCertificateSecret)
		C.ResyncTLS(pps.Namespace, pps.Spec.TLSCertificateSecret)
	}

	return &propsy.ListenerConfig{
		Name:   listenerName,
		Listen: pps.Spec.Listen,
		VirtualHosts: []*propsy.VirtualHost{{
			Name:    vhostName,
			Domains: domains,
			Routes:  []*propsy.RouteConfig{C.NewRouteConfig(pps)},
		}},
		Type:            propsyType,
		TrackedLocality: C.locality.Zone,
		TLSSecret:       tlsData,
	}
}

func (C *ProPsyController) ResyncEndpoints(pps *propsyv1.ProPsyService) {
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
}

func (C *ProPsyController) PPSAdded(pps *propsyv1.ProPsyService) {
	// TODO support disabled flag
	var nodes []*propsy.NodeConfig
	for i := 0; i < len(pps.Spec.Nodes); i++ {
		node := C.ppsCache.GetOrCreateNode(pps.Spec.Nodes[i])
		nodes = append(nodes, node)
		logrus.Infof("Node: %p, %s", node, node.NodeName)
	}

	listenerConfig := C.NewListenerConfig(pps)
	logrus.Debugf("Generated a new listener: %+v", listenerConfig)
	if listenerConfig == nil {
		return
	}

	C.ppsCache.RegisterEndpointSet(listenerConfig.VirtualHosts[0].Routes[0].Clusters[0].EndpointConfig, nodes)
	if pps.Spec.CanaryService != "" {
		C.ppsCache.RegisterEndpointSet(listenerConfig.VirtualHosts[0].Routes[0].Clusters[1].EndpointConfig, nodes)
	}

	for node := range nodes {
		nodes[node].AddListener(listenerConfig)
		if pps.Spec.TLSCertificateSecret != "" {
			C.ppsCache.AddTLSWatch(pps.Namespace, pps.Spec.TLSCertificateSecret, nodes[node])
		}
	}

	C.ResyncEndpoints(pps)

	for node := range nodes {
		logrus.Infof("Dispatching update to %s", nodes[node].NodeName)
		nodes[node].Update()
	}
	logrus.Infof("cache content: %+v", C.ppsCache)
	C.ppsCache.DumpNodes()
}

func (C *ProPsyController) PPSRemoved(pps *propsyv1.ProPsyService) {
	propsyType := GetProxyType(pps.Spec.Type)
	domains := []string{"*"}
	vhostName := propsy.GenerateVHostName(domains)
	listenerName := propsy.GenerateListenerName(pps.Spec.Listen, propsyType)
	routeName, _ := propsy.GenerateRouteName(pps.Spec.PathPrefix)

	for i := range pps.Spec.Nodes {
		node := C.ppsCache.GetOrCreateNode(pps.Spec.Nodes[i])
		lis := node.FindListener(listenerName)
		if lis != nil {
			if pps.Spec.CanaryService != "" {
				lis.SafeRemove(vhostName, routeName, propsy.GenerateUniqueEndpointName(C.locality, pps.Namespace, pps.Spec.CanaryService))
			}
			lis.SafeRemove(vhostName, routeName, propsy.GenerateUniqueEndpointName(C.locality, pps.Namespace, pps.Spec.Service))

			logrus.Debugf("Remaining vhosts: %d", len(lis.VirtualHosts))
			if len(lis.VirtualHosts) == 0 {
				lis.Free()
				node.RemoveListener(lis.Name)
				propsy.RemoveFromEnvoy(node)
			} else {
				node.Update()
			}
		}
	}
}

func (C *ProPsyController) PPSChanged(old *propsyv1.ProPsyService, new *propsyv1.ProPsyService) {
	// TODO
	logrus.Printf("Content of cache: %+v", C.ppsCache)
	if reflect.DeepEqual(old, new) {
		return
	}

	C.PPSRemoved(old)
	C.PPSAdded(new)

	for i := range new.Spec.Nodes {
		C.ppsCache.GetOrCreateNode(new.Spec.Nodes[i]).Update()
		if old.Spec.Service != new.Spec.Service || old.Spec.CanaryService != new.Spec.CanaryService {
			C.ResyncEndpoints(new)
		}
	}

}

func DoesListContain(haystack []string, needle string) bool {
	for i := range haystack {
		if haystack[i] == needle {
			return true
		}
	}
	return false
}
