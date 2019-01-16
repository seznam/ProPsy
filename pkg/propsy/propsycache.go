package propsy

import (
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"sync"
	"time"
)

type ProPsyCache struct {
	queue   workqueue.RateLimitingInterface
	stopper chan struct{}

	// mapped by node name
	nodeConfigs map[string]*NodeConfig

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
