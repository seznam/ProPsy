package propsy

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"strconv"
	"strings"
	"sync"
)

type NodeConfig struct {
	NodeName  string
	Listeners []*ListenerConfig
}

type ProxyType int

const (
	HTTP ProxyType = iota
	TCP
)

type ListenerConfig struct {
	Name         string
	Listen       string
	VirtualHosts []*VirtualHost
	Type         ProxyType
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
	Name           string
	ConnectTimeout int
	EndpointConfig *EndpointConfig
	Weight         int
	IsCanary       bool
}

type EndpointConfig struct {
	Lock        sync.Mutex
	Name        string
	ServicePort int // used only internally
	Locality    *Locality
	Endpoints   []*Endpoint
}

type Endpoint struct {
	Host    string
	Weight  int
	Healthy bool
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
	logrus.Debugf("Adding a listener to node %s: %s", N.NodeName, l.Name)
	if N.FindListener(l.Name) != nil {
		N.FindListener(l.Name).AddVHosts(l.VirtualHosts)
	} else {
		N.Listeners = append(N.Listeners, l)
	}
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
	logrus.Debugf("Removing listener %s from node %s", s, N.NodeName)
	for i := range N.Listeners {
		if N.Listeners[i].Name == s {
			N.Listeners[i] = N.Listeners[len(N.Listeners)-1]
			N.Listeners = N.Listeners[:len(N.Listeners)-1]
			return
		}
	}
	logrus.Debugf("No such listener found!")
}

func (V *VirtualHost) AddRoute(r *RouteConfig) {
	if V.FindRoute(r.Name) != nil {
		V.FindRoute(r.Name).AddClusters(r.Clusters)
	} else {
		V.Routes = append(V.Routes, r)
	}
}

func (V *VirtualHost) RemoveRoute(name string) {
	for route := range V.Routes {
		if V.Routes[route].Name == name {
			V.Routes[route] = V.Routes[len(V.Routes)-1]
			V.Routes = V.Routes[:len(V.Routes)-1]
			return
		}
	}
}

func (V *VirtualHost) FindRoute(name string) *RouteConfig {
	for route := range V.Routes {
		if V.Routes[route].Name == name {
			return V.Routes[route]
		}
	}

	logrus.Debugf("Found no route %s", name)
	return nil
}

func (V *VirtualHost) Free() {
	for r := range V.Routes {
		V.Routes[r].Free()
	}
}

func (V *VirtualHost) AddRoutes(configs []*RouteConfig) {
	for i := range configs {
		V.AddRoute(configs[i])
	}
}

func (R *RouteConfig) AddCluster(c *ClusterConfig) {
	if i := R.FindCluster(c.Name); i != nil {
		for ep := range c.EndpointConfig.Endpoints {
			i.EndpointConfig.AddEndpoint(c.EndpointConfig.Endpoints[ep].Host, c.EndpointConfig.Endpoints[ep].Weight, c.EndpointConfig.Endpoints[ep].Healthy)
		}
	} else {
		R.Clusters = append(R.Clusters, c)
	}
}

func (R *RouteConfig) RemoveCluster(name string) {
	for cluster := range R.Clusters {
		if R.Clusters[cluster].Name == name {
			R.Clusters[cluster] = R.Clusters[len(R.Clusters)-1]
			R.Clusters = R.Clusters[:len(R.Clusters)-1]
			return
		}
	}
}

func (R *RouteConfig) FindCluster(name string) *ClusterConfig {
	for cluster := range R.Clusters {
		if R.Clusters[cluster].Name == name {
			return R.Clusters[cluster]
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

func (R *RouteConfig) CalculateWeights() (
	totalWeight int, localZoneWeight int, otherZoneWeight int, canariesWeight int, connectTimeout int) {
	otherZoneCount := 0
	// find the total sum of weights that are not our cluster and our clusters as well
	for c := range R.Clusters {
		_cluster := R.Clusters[c]
		if _cluster.EndpointConfig == nil {
			continue
		}
		if _cluster.EndpointConfig.Locality.Zone == LocalZone && !_cluster.IsCanary {
			localZoneWeight = _cluster.Weight
			connectTimeout = _cluster.ConnectTimeout
		} else if ! _cluster.IsCanary {
			otherZoneWeight += _cluster.Weight
			otherZoneCount++
		} else if _cluster.IsCanary && _cluster.EndpointConfig.Locality.Zone == LocalZone {
			canariesWeight += _cluster.Weight // should be no more than one
		}
	}

	// do magic with weights
	if localZoneWeight >= 100 {
		otherZoneWeight = 0
		localZoneWeight = 100
	} else {
		otherZoneWeight = 100 - localZoneWeight // todo change the maths to be an actual percentage of the rest
	}

	totalWeight = localZoneWeight + otherZoneCount * otherZoneWeight + canariesWeight // canaries are separated

	return totalWeight, localZoneWeight, otherZoneWeight, canariesWeight, connectTimeout
}

func (R *RouteConfig) AddClusters(configs []*ClusterConfig) {
	for i := range configs {
		R.AddCluster(configs[i])
	}
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

func (R *ListenerConfig) AddVHosts(hosts []*VirtualHost) {
	for i := range hosts {
		R.AddVHost(hosts[i])
	}
}

func (R *ListenerConfig) AddVHost(host *VirtualHost) {
	if R.FindVHost(host.Name) != nil {
		R.FindVHost(host.Name).AddRoutes(host.Routes)
	} else {
		R.VirtualHosts = append(R.VirtualHosts, host)
	}
}

func (C *ClusterConfig) Free() {
	for i := range C.EndpointConfig.Endpoints {
		C.EndpointConfig.Endpoints[i] = nil
	}

	C.EndpointConfig.Endpoints = []*Endpoint{}
}

func GenerateUniqueEndpointName(locality *Locality, namespace, name string) string {
	return fmt.Sprintf("%s-%s-%s", locality.Zone, namespace, name)
}

func GenerateUniqConfigName(namespace, name string) string {
	return fmt.Sprintf("%s-%s", namespace, name)
}

func (E *EndpointConfig) Clear() {
	E.Endpoints = []*Endpoint{}
}

func (E *EndpointConfig) AddEndpoint(host string, weight int, healthy bool) {
	E.RemoveEndpoint(host) // force remove if it exists to avoid duplicating
	E.Endpoints = append(E.Endpoints, &Endpoint{Host: host, Weight: weight, Healthy: healthy})
}

func (E *EndpointConfig) RemoveEndpoint(host string) {
	removalI := -1
	for i := 0; i < len(E.Endpoints); i++ {
		if E.Endpoints[i].Host == host {
			removalI = i
			break
		}
	}

	if removalI != -1 { // never remove from an array you're iterating over, although here it may be safe since we break immediately?
		logrus.Debugf("Removing endpoint %i (host: %s)", removalI, host)
		E.Endpoints[removalI] = E.Endpoints[len(E.Endpoints)-1]
		E.Endpoints = E.Endpoints[:len(E.Endpoints)-1]
	}
}

func (E *EndpointConfig) GetEndpoint(host string) *Endpoint {
	for i := 0; i < len(E.Endpoints); i++ {
		if E.Endpoints[i].Host == host {
			return E.Endpoints[i]
		}
	}

	return nil
}

func (L *ListenerConfig) GenerateListenParts() (host string, port int64) {
	parts := strings.Split(L.Listen, ":")
	port, _ = strconv.ParseInt(parts[0], 10, 32)
	if len(parts) > 1 {
		port, _ = strconv.ParseInt(parts[1], 10, 32)
		host = parts[0]
	} else {
		host = "0.0.0.0"
	}

	if host == "0" {
		host = "0.0.0.0"
	}

	return
}

type Locality struct {
	// Region string // region is fixed, we only change zone
	Zone string
}
