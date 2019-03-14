package propsy

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"sync"
	"time"
)

type TlsData struct {
	Certificate []byte
	Key []byte
}

type ProPsyCache struct {
	queue   workqueue.RateLimitingInterface
	stopper chan struct{}

	// mapped by node name
	nodeConfigs map[string]*NodeConfig

	// mapped by TLS secret name
	tlsNodes              map[string][]*NodeConfig
	tlsSecrets            map[string]*TlsData

	// mapped by endpoint key
	endpointConfigsByName map[string]*EndpointConfig
	endpointNodes         map[string][]*NodeConfig

	mu             sync.Mutex
	MutexEndpoints sync.Mutex
}

func NewProPsyCache() *ProPsyCache {
	cache := ProPsyCache{
		queue:                 workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeConfigs:           map[string]*NodeConfig{},
		endpointConfigsByName: map[string]*EndpointConfig{},
		endpointNodes:         map[string][]*NodeConfig{},
		tlsNodes:              map[string][]*NodeConfig{},
		tlsSecrets:            map[string]*TlsData{},
	}

	return &cache
}

// processes the whole queue one time
func (P *ProPsyCache) ProcessQueueOnce() {
	for P.queue.Len() > 0 {
		P.processQueueItem()
	}
}

func (P *ProPsyCache) Run() {
	wait.Until(P.runQueueWorker, time.Second, P.stopper)
}

func (P *ProPsyCache) runQueueWorker() {
	for P.processQueueItem() {

	}
}

func (P *ProPsyCache) processQueueItem() bool {

	data, done := P.queue.Get()
	if done {
		return false
	}
	defer P.queue.Done(data)

	// todo do something actually
	return true
}

func (P *ProPsyCache) GetEndpointSetByEndpoint(endpointName string) (*EndpointConfig, []*NodeConfig) {
	if ecs, ok := P.endpointConfigsByName[endpointName]; ok {
		return ecs, P.endpointNodes[endpointName]
	}

	return nil, nil
}

func (P *ProPsyCache) RegisterEndpointSet(cfg *EndpointConfig, nodes []*NodeConfig) {
	P.mu.Lock()
	defer P.mu.Unlock()

	if _, ok := P.endpointNodes[cfg.Name]; ok {
		P.endpointNodes[cfg.Name] = append(P.endpointNodes[cfg.Name], nodes...)
	} else {
		P.endpointNodes[cfg.Name] = nodes
	}
	P.endpointConfigsByName[cfg.Name] = cfg
}

func (P *ProPsyCache) AddTLSWatch(secretNamespace, secretName string, node *NodeConfig) {
	P.GetOrCreateTLS(secretNamespace, secretName)

	secretName = fmt.Sprintf("%s__%s", secretNamespace, secretName)

	if _, ok := P.tlsNodes[secretName]; !ok {
		P.tlsNodes[secretName] = []*NodeConfig{node}
	} else {
		P.tlsNodes[secretName] = append(P.tlsNodes[secretName], node)
	}

	logrus.Debugf("Successfully added node %s to TLS %s", node.NodeName, secretName)
}

func (P *ProPsyCache) RemoveTLSWatch(secretNamespace, secretName string, node *NodeConfig) {
	secretName = fmt.Sprintf("%s__%s", secretNamespace, secretName)
	if _, ok := P.tlsNodes[secretName]; !ok {
		return
	}

	for i := range P.tlsNodes[secretName] {
		if P.tlsNodes[secretName][i] == node {
			P.tlsNodes[secretName][i] = P.tlsNodes[secretName][len(P.tlsNodes[secretName])-1]
			logrus.Debugf("Successfully removed node %s from TLS %s", node.NodeName, secretName)
			return
		}
	}
	logrus.Warnf("Failed to remove node %s from TLS %s", node.NodeName, secretName)
}

func (C *ProPsyCache) UpdateTLS(secretNamespace, secretName string, certificate, key []byte) bool {
	tls := C.GetTls(secretNamespace, secretName)
	if tls == nil {
		return false
	}

	logrus.Debugf("Updating TLS %s to new data", secretName)
	tls.Certificate = certificate
	tls.Key = key

	secretName = fmt.Sprintf("%s__%s", secretNamespace, secretName)
	for i := range C.tlsNodes[secretName] {
		C.tlsNodes[secretName][i].Update()
	}

	return true
}

func (P *ProPsyCache) Cleanup() {
	// TODO cleanup
	// clean up all the resources that are no longer used to free some memory and prevent eventual memory leaks
}

func (P *ProPsyCache) GetOrCreateNode(name string) *NodeConfig {
	P.mu.Lock()
	defer P.mu.Unlock()

	if node, ok := P.nodeConfigs[name]; ok {
		return node
	}

	node := &NodeConfig{NodeName: name}
	P.nodeConfigs[name] = node
	return node
}

func (P *ProPsyCache) GetOrCreateTLS(namespace, name string) *TlsData {
	P.mu.Lock()
	defer P.mu.Unlock()

	name = fmt.Sprintf("%s__%s", namespace, name)

	if tls, ok := P.tlsSecrets[name]; ok {
		return tls
	}

	secret := &TlsData{}
	P.tlsSecrets[name] = secret
	return secret
}

func (P *ProPsyCache) GetTls(namespace, name string) *TlsData {
	name = fmt.Sprintf("%s__%s", namespace, name)

	if tls, ok := P.tlsSecrets[name]; ok {
		return tls
	}

	return nil
}

func (P *ProPsyCache) ClearPPS(ppsName string, nodes []string, canaryName string) {
	P.mu.Lock()
	defer P.mu.Unlock()

	for i := range nodes {
		delete(P.nodeConfigs, nodes[i])
	}
}

func (P *ProPsyCache) RemoveEndpointSet(s string, nodes []*NodeConfig) {
	P.mu.Lock()
	defer P.mu.Unlock()

	removed := 0
	for i := range P.endpointNodes[s] {
		for j := range nodes {
			if P.endpointNodes[s][i] != nil && P.endpointNodes[s][i].NodeName == nodes[j].NodeName {
				removed = removed + 1
				P.endpointNodes[s][i] = P.endpointNodes[s][len(P.endpointNodes[s])-1]
			}
		}
	}

	P.endpointNodes[s] = P.endpointNodes[s][:len(P.endpointNodes[s])-removed]

	delete(P.endpointConfigsByName, s)
}

func (P *ProPsyCache) DumpNodes() {
	for i := range P.nodeConfigs {
		logrus.Infof("Node in pps cache: %p, %s", &P.nodeConfigs, P.nodeConfigs[i].NodeName)
	}
}
