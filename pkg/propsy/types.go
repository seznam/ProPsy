package propsy

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"sync"
)

type NodeConfig struct {
	NodeName  string
	Listeners []*ListenerConfig
}

type ListenerConfig struct {
	//	TrackedConfig
	Name         string
	Listen       string
	VirtualHosts []*VirtualHost
}

type RouteConfig struct {
	Name     string
	Clusters []*ClusterConfig
}

type VirtualHost struct {
	Name    string
	Domains []string
	Routes  []*RouteConfig
}

type ClusterConfig struct {
	//	TrackedConfig
	Name           string
	ConnectTimeout int
	EndpointConfig *EndpointConfig
	Weight         int
}

type EndpointConfig struct {
	Lock        sync.Mutex
	Name        string
	ServicePort int // used only internally
	Endpoints   map[*Locality][]*Endpoint
}

type Endpoint struct {
	Host   string
	Weight int
}

func (N *NodeConfig) Update() {
	GenerateEnvoyConfig(N)
}

func (N *NodeConfig) Free() {
	// free all the resources to avoid memleaks by keeping refs somewhere
	logrus.Debugf("Removing everything from node: %s", N.NodeName)
	for i := range N.Listeners {
		N.Listeners[i].Free()
	}
	N.Listeners = []*ListenerConfig{}
}

func (N *NodeConfig) AddListener(l *ListenerConfig) {
	logrus.Debugf("Adding a listener to node: %s", N.NodeName)
	N.Listeners = append(N.Listeners, l)
}

func (N *NodeConfig) FindListener(name string) *ListenerConfig {
	for i := range N.Listeners {
		if N.Listeners[i].Name == name {
			return N.Listeners[i]
		}
	}

	logrus.Debugf("Found no listener %s", name)
	return nil
}

func (N *NodeConfig) RemoveListener(s string) {
	for i := range N.Listeners {
		if N.Listeners[i].Name == s {
			N.Listeners[i] = N.Listeners[len(N.Listeners)-1]
			N.Listeners = N.Listeners[:len(N.Listeners)-1]
			return
		}
	}
}

func (L *VirtualHost) AddRoute(r *RouteConfig) {
	L.Routes = append(L.Routes, r)
}

func (L *VirtualHost) RemoveRoute(name string) {
	for route := range L.Routes {
		if L.Routes[route].Name == name {
			L.Routes[route] = L.Routes[len(L.Routes)-1]
			L.Routes = L.Routes[:len(L.Routes)-1]
			return
		}
	}
}

func (L *VirtualHost) FindRoute(name string) *RouteConfig {
	for route := range L.Routes {
		if L.Routes[route].Name == name {
			return L.Routes[route]
		}
	}

	logrus.Debugf("Found no route %s", name)
	return nil
}

func (L *VirtualHost) Free() {
	for r := range L.Routes {
		L.Routes[r].Free()
	}
}

func (V *RouteConfig) AddCluster(c *ClusterConfig) {
	V.Clusters = append(V.Clusters, c)
}

func (V *RouteConfig) RemoveCluster(name string) {
	for cluster := range V.Clusters {
		if V.Clusters[cluster].Name == name {
			V.Clusters[cluster] = V.Clusters[len(V.Clusters)-1]
			V.Clusters = V.Clusters[:len(V.Clusters)-1]
			return
		}
	}
}

func (V *RouteConfig) FindCluster(name string) *ClusterConfig {
	for cluster := range V.Clusters {
		if V.Clusters[cluster].Name == name {
			return V.Clusters[cluster]
		}
	}

	logrus.Debugf("Found no cluster %s", name)
	return nil
}

func (R *RouteConfig) Free() {
	for i := range R.Clusters {
		R.Clusters[i].Free()
		R.Clusters[i] = nil
	}
}

func (R *RouteConfig) TotalWeight() int {
	totalWeight := 0
	for i := 0; i < len(R.Clusters); i++ {
		totalWeight += R.Clusters[i].Weight

	}

	return totalWeight
}

func (R *ListenerConfig) FindVHost(name string) *VirtualHost {
	for i := range R.VirtualHosts {
		if R.VirtualHosts[i].Name == name {
			return R.VirtualHosts[i]
		}
	}

	logrus.Debugf("Found no vhost %s", name)
	return nil
}

func (L *ListenerConfig) RemoveVHost(name string) {
	for i := range L.VirtualHosts {
		if L.VirtualHosts[i].Name == name {
			L.VirtualHosts[i] = L.VirtualHosts[len(L.VirtualHosts)-1]
			L.VirtualHosts = L.VirtualHosts[:len(L.VirtualHosts)-1]
			return
		}
	}
}

func (L *ListenerConfig) Free() {
	for i := range L.VirtualHosts {
		L.VirtualHosts[i].Free()
	}
	L.VirtualHosts = nil
}

func (C *ClusterConfig) Free() {
	for i := range C.EndpointConfig.Endpoints {
		C.EndpointConfig.Endpoints[i] = nil
	}

	C.EndpointConfig.Endpoints = map[*Locality][]*Endpoint{}
}

func GenerateUniqueEndpointName(locality *Locality, namespace, name string) string {
	return fmt.Sprintf("%s-%s-%s", locality.Zone, namespace, name)
}

func (E *EndpointConfig) Clear() {
	for locality := range E.Endpoints {
		delete(E.Endpoints, locality)
	}
}

func (E *EndpointConfig) ClearLocality(locality *Locality) {
	if _, ok := E.Endpoints[locality]; ok {
		E.Endpoints[locality] = nil
	}
}

func (E *EndpointConfig) AddEndpoint(locality *Locality, host string, weight int) {
	E.RemoveEndpoint(locality, host) // force remove if it exists to avoid duplicating
	if _, ok := E.Endpoints[locality]; ok {
		E.Endpoints[locality] = append(E.Endpoints[locality], &Endpoint{Host: host, Weight: weight})
	} else {
		E.Endpoints[locality] = []*Endpoint{{
			Host:   host,
			Weight: weight,
		}}
	}
}

func (E *EndpointConfig) RemoveEndpoint(locality *Locality, host string) {
	if _, ok := E.Endpoints[locality]; ok {
		removalI := -1
		for i := 0; i < len(E.Endpoints[locality]); i++ {
			if E.Endpoints[locality][i].Host == host {
				removalI = i
				break
			}
		}

		if removalI != -1 { // never remove from an array you're iterating over, although here it may be safe since we break immediately?
			logrus.Debugf("Removing endpoint %i (host: %s)", removalI, host)
			E.Endpoints[locality][removalI] = E.Endpoints[locality][len(E.Endpoints[locality])-1]
			E.Endpoints[locality] = E.Endpoints[locality][:len(E.Endpoints[locality])-1]
		}
	}
}

func (E *EndpointConfig) GetEndpoint(locality *Locality, host string) *Endpoint {
	if _, ok := E.Endpoints[locality]; ok {
		for i := 0; i < len(E.Endpoints[locality]); i++ {
			if E.Endpoints[locality][i].Host == host {
				return E.Endpoints[locality][i]
			}
		}
	}

	return nil
}

type Locality struct {
	// Region string // region is fixed, we only change zone
	Zone string
}
