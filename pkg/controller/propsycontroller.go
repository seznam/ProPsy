package controller

import (
	"errors"
	propsyv1 "github.com/seznam/ProPsy/pkg/apis/propsy/v1"
	propsyclient "github.com/seznam/ProPsy/pkg/client/clientset/versioned"
	ppsv1 "github.com/seznam/ProPsy/pkg/client/clientset/versioned/typed/propsy/v1"
	informerext "github.com/seznam/ProPsy/pkg/client/informers/externalversions"
	ppslisterv1 "github.com/seznam/ProPsy/pkg/client/listers/propsy/v1"
	"github.com/seznam/ProPsy/pkg/propsy"
	"github.com/sirupsen/logrus"
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

// a single propsy controller that reads from kubernetes
type ProPsyController struct {
	kubeClient kubernetes.Interface
	locality   *propsy.Locality
	ppsCache   *propsy.ProPsyCache

	secretGetter       corev1.SecretsGetter
	secretLister       listerv1.SecretLister
	secretListerSynced cache.InformerSynced

	ppsGetter       ppsv1.ProPsyServicesGetter
	ppsLister       ppslisterv1.ProPsyServiceLister
	ppsListerSynced cache.InformerSynced

	endpointControllers []*EndpointController
}

func NewProPsyController(endpointClient kubernetes.Interface, crdClient propsyclient.Interface, locality *propsy.Locality, ppsCache *propsy.ProPsyCache, endpointControllers []*EndpointController) (*ProPsyController, error) {
	sharedInformers := informers.NewSharedInformerFactory(endpointClient, 10*time.Second)
	secretInformer := sharedInformers.Core().V1().Secrets()

	var propsy ProPsyController

	if crdClient == nil {
		return nil, errors.New("missing crd client")
	}
	customInformers := informerext.NewSharedInformerFactory(crdClient, 10*time.Second)
	propsyInformer := customInformers.Propsy().V1().ProPsyServices()
	propsy = ProPsyController{
		kubeClient: endpointClient,
		locality:   locality,
		ppsCache:   ppsCache,

		secretGetter:       endpointClient.CoreV1(),
		secretLister:       secretInformer.Lister(),
		secretListerSynced: secretInformer.Informer().HasSynced,

		ppsGetter:       crdClient.PropsyV1(),
		ppsLister:       propsyInformer.Lister(),
		ppsListerSynced: propsyInformer.Informer().HasSynced,

		endpointControllers: endpointControllers,
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
	if !cache.WaitForCacheSync(stop, C.ppsListerSynced) {
		log.Fatal("Error waiting to sync initial cache")
		return
	}

	log.Print("Finished syncing initial cache")
}

func (C *ProPsyController) SecretAdded(secret *v1.Secret) {
	if C.locality.Zone != propsy.LocalZone {
		return
	}
	logrus.Debugf("Secret added: %s:%s", secret.Namespace, secret.Name)
	C.ppsCache.UpdateTLS(secret.Namespace, secret.Name, secret.Data["tls.crt"], secret.Data["tls.key"])
}

func (C *ProPsyController) SecretRemoved(secret *v1.Secret) {
	if C.locality.Zone != propsy.LocalZone {
		return
	}
	logrus.Debugf("Secret removed: %s:%s", secret.Namespace, secret.Name)
	C.ppsCache.UpdateTLS(secret.Namespace, secret.Name, []byte{}, []byte{})
}

func (C *ProPsyController) SecretChanged(old, new *v1.Secret) {
	if C.locality.Zone != propsy.LocalZone {
		return
	}
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

func (C *ProPsyController) NewCluster(pps *propsyv1.ProPsyService, priority int, isCanary bool) *propsy.ClusterConfig {
	var endpointName string
	var percent int
	if isCanary {
		endpointName = propsy.GenerateUniqueEndpointName(priority, pps.Namespace, pps.Spec.CanaryService)
		percent = pps.Spec.CanaryPercent
	} else {
		endpointName = propsy.GenerateUniqueEndpointName(priority, pps.Namespace, pps.Spec.Service)
		percent = pps.Spec.Percent
	}

	endpointConfig := propsy.EndpointConfig{
		Name:        endpointName,
		ServicePort: pps.Spec.ServicePort,
		Endpoints:   []*propsy.Endpoint{},
	}

	return &propsy.ClusterConfig{
		ConnectTimeout: pps.Spec.ConnectTimeout,
		Name:           endpointName,
		Weight:         percent,
		EndpointConfig: &endpointConfig,
		IsCanary:       isCanary,
		MaxRequests:    pps.Spec.MaxRequestsPerConnection,
		Priority:       priority,
	}
}

func (C *ProPsyController) NewRouteConfig(pps *propsyv1.ProPsyService) *propsy.RouteConfig {
	var clusterConfigs []*propsy.ClusterConfig

	for i := range C.endpointControllers {
		clusterConfig := C.NewCluster(pps, C.endpointControllers[i].Priority, false)
		clusterConfigCanary := C.NewCluster(pps, C.endpointControllers[i].Priority, true)

		clusterConfigs = append(clusterConfigs, clusterConfig)
		if pps.Spec.CanaryService != "" {
			clusterConfigs = append(clusterConfigs, clusterConfigCanary)
		}
	}

	routeName, path := propsy.GenerateRouteName(pps.Spec.PathPrefix)

	timeout := time.Duration(pps.Spec.Timeout) * time.Millisecond

	return &propsy.RouteConfig{
		Name:          routeName,
		Clusters:      clusterConfigs,
		PathPrefix:    path,
		PrefixRewrite: pps.Spec.PrefixRewrite,
		Timeout:       timeout,
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
	if pps.Spec.TLSCertificateSecret != "" && C.locality.Zone == propsy.LocalZone && (pps.Spec.PathPrefix == "" || pps.Spec.PathPrefix == "/") {
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
	for ctrl := range C.endpointControllers {
		C.endpointControllers[ctrl].ResyncEndpoints(pps.Namespace, pps.Spec.Service, pps.Spec.CanaryService)
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

	for i := range listenerConfig.VirtualHosts[0].Routes[0].Clusters {
		C.ppsCache.RegisterEndpointSet(listenerConfig.VirtualHosts[0].Routes[0].Clusters[i].EndpointConfig, nodes)
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
			for ec := range C.endpointControllers {
				if pps.Spec.CanaryService != "" {
					lis.SafeRemove(vhostName, routeName, propsy.GenerateUniqueEndpointName(C.endpointControllers[ec].Priority, pps.Namespace, pps.Spec.CanaryService), C.locality.Zone)
				}
				lis.SafeRemove(vhostName, routeName, propsy.GenerateUniqueEndpointName(C.endpointControllers[ec].Priority, pps.Namespace, pps.Spec.Service), C.locality.Zone)

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
}

func (C *ProPsyController) PPSChanged(old *propsyv1.ProPsyService, new *propsyv1.ProPsyService) {
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
