package propsy

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"strconv"
	"strings"
	"sync"
	"time"
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

type HealthCheckType int

const (
	HTTPHealthCheck HealthCheckType = iota
	HTTP2HealthCheck
	TCPHealthCheck
	GRPCHealthCheck
)

type HealthCheckConfig struct {
	Timeout           time.Duration
	Interval          time.Duration
	UnhealthyTreshold int
	HealthyTreshold   int
	ReuseConnection   bool
	HealthChecker     HealthCheckType
	HTTPPath          string
	HTTPHost          string
}

type ListenerConfig struct {
	Name            string
	Listen          string
	VirtualHosts    []*VirtualHost
	Type            ProxyType
	TrackedLocality []string
	TLSSecret       *TlsData

	mu sync.Mutex
}

func (L *ListenerConfig) String() string {
	return fmt.Sprintf("Name: %s, Listen: %s, Type: %d, TrackedLocality: %s, TLSSecret: \n%v\nVirtualHosts: \n%+v",
		L.Name, L.Listen, L.Type, L.TrackedLocality, L.TLSSecret, L.VirtualHosts)
}

type RouteConfig struct {
	Name          string
	Clusters      []*ClusterConfig
	PathPrefix    string
	PrefixRewrite string
	Timeout       time.Duration
}

func (R *RouteConfig) String() string {
	return fmt.Sprintf("Name: %s, PathPrefix: %s, PrefixRewrite: %s, Timeout: %s, Clusters:\n%+v",
		R.Name, R.PathPrefix, R.PrefixRewrite, R.Timeout.String(), R.Clusters)
}

type VirtualHost struct {
	Name    string
	Domains []string
	Routes  []*RouteConfig
}

func (V *VirtualHost) String() string {
	return fmt.Sprintf("Name: %s, Domains: %v, Routes:\n%+v",
		V.Name, V.Domains, V.Routes)
}

type ClusterConfig struct {
	Name           string
	ConnectTimeout int
	EndpointConfig *EndpointConfig
	Weight         int
	IsCanary       bool
	MaxRequests    int
	Priority       int
	HealthCheck    *HealthCheckConfig
}

func (C *ClusterConfig) String() string {
	return fmt.Sprintf("Name: %s, ConnectTimeout: %d, Weight: %d, MaxRequests: %d, IsCanary: %v, EndpointConfig: %+v",
		C.Name, C.ConnectTimeout, C.Weight, C.MaxRequests, C.IsCanary, C.EndpointConfig)
}

type EndpointConfig struct {
	Lock        sync.Mutex
	Name        string
	ServicePort int // used only internally
	Endpoints   []*Endpoint
	Locality    *Locality
}

func (E *EndpointConfig) String() string {
	return fmt.Sprintf("Name: %s, ServicePort: %d, Endpoints: %+v",
		E.Name, E.ServicePort, E.Endpoints)
}

type Endpoint struct {
	Host    string
	Weight  int
	Healthy bool
}

func (E *Endpoint) String() string {
	return fmt.Sprintf("Host: %s, Weight: %d, Healthy: %v",
		E.Host, E.Weight, E.Healthy)
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
		listener := N.FindListener(l.Name)
		// force local zone to become master locality for this listener if possible
		if !listener.IsTrackedBy(LocalZone) && l.IsTrackedBy(LocalZone) {
			// remove the old one
			N.RemoveListener(listener.Name)
			// force a local one to be added
			N.AddListener(l)
			// and now swap them around
			xl := listener
			listener = l
			l = xl
		}

		listener.AddVHosts(l.VirtualHosts)
		for i := range l.TrackedLocality {
			listener.AddTracker(l.TrackedLocality[i])
		} // copy over listeners
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

func (V *VirtualHost) SafeRemoveRoute(route string, clusterName string) {
	R := V.FindRoute(route)
	if R == nil {
		return
	}

	if clusterName != "" {
		R.RemoveCluster(clusterName)
	}

	if len(R.Clusters) == 0 {
		V.RemoveRoute(route)
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

	R.Clusters = []*ClusterConfig{}
}

func (R *RouteConfig) GetLocalBestCluster(canary bool) *ClusterConfig {
	var bestCluster *ClusterConfig
	for c := range R.Clusters {
		_cluster := R.Clusters[c]
		if _cluster.IsLocalCluster() &&
			(bestCluster == nil || _cluster.Priority < bestCluster.Priority) &&
			_cluster.IsCanary == canary {
			bestCluster = _cluster
		}
	}

	return bestCluster
}

func (R *RouteConfig) CalculateWeights() (
	totalWeight, localZoneWeight, otherZonesWeight, canariesWeight, connectTimeout, maxRequests int) {
	otherZoneCount := 0
	connectTimeout = 1 // set sane default so envoy doesn't freak out

	bestCluster, bestClusterCanary := R.GetLocalBestCluster(false), R.GetLocalBestCluster(true)

	// find the total sum of weights that are not our cluster and our clusters as well
	for c := range R.Clusters {
		_cluster := R.Clusters[c]
		if _cluster.EndpointConfig == nil {
			continue
		}
		if bestCluster == _cluster {
			localZoneWeight = _cluster.Weight
			connectTimeout = _cluster.ConnectTimeout
			maxRequests = _cluster.MaxRequests
		} else if bestClusterCanary == _cluster && _cluster.HasEndpoints() {
			canariesWeight += _cluster.Weight // should be no more than one
		} else if !_cluster.IsCanary && _cluster.HasEndpoints() {
			otherZonesWeight += _cluster.Weight
			otherZoneCount++
		}
	}

	// do magic with weights
	if localZoneWeight >= 100 {
		otherZonesWeight = 0
		localZoneWeight = 100
	} else {
		otherZonesWeight = 100 - localZoneWeight // todo change the maths to be an actual percentage of the rest
	}

	totalWeight = localZoneWeight + otherZoneCount*otherZonesWeight + canariesWeight // canaries are separated

	return totalWeight, localZoneWeight, otherZonesWeight, canariesWeight, connectTimeout, maxRequests
}

func (R *RouteConfig) AddClusters(configs []*ClusterConfig) {
	for i := range configs {
		R.AddCluster(configs[i])
	}
}

func (R *RouteConfig) GenerateUniqueRouteName() string {
	return strings.Replace(R.PathPrefix, "/", "-", -1)
}

func (L *ListenerConfig) FindVHost(name string) *VirtualHost {
	for i := range L.VirtualHosts {
		if L.VirtualHosts[i].Name == name {
			return L.VirtualHosts[i]
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

func (L *ListenerConfig) AddVHosts(hosts []*VirtualHost) {
	for i := range hosts {
		L.AddVHost(hosts[i])
	}
}

func (L *ListenerConfig) AddVHost(host *VirtualHost) {
	if L.FindVHost(host.Name) != nil {
		L.FindVHost(host.Name).AddRoutes(host.Routes)
	} else {
		L.VirtualHosts = append(L.VirtualHosts, host)
	}
}

func (L *ListenerConfig) IsTrackedBy(zone string) bool {
	L.mu.Lock()
	defer L.mu.Unlock()

	for i := range L.TrackedLocality {
		if L.TrackedLocality[i] == zone {
			return true
		}
	}

	return false
}

func (L *ListenerConfig) RemoveTracker(zone string) {
	L.mu.Lock()
	defer L.mu.Unlock()

	for i := range L.TrackedLocality {
		if L.TrackedLocality[i] == zone {
			L.TrackedLocality[i] = L.TrackedLocality[len(L.TrackedLocality)-1]  // move the last item here
			L.TrackedLocality = L.TrackedLocality[0 : len(L.TrackedLocality)-1] // drop the last item from the list
			break
		}
	}
}

func (L *ListenerConfig) AddTracker(zone string) {
	L.mu.Lock()
	defer L.mu.Unlock()

	for i := range L.TrackedLocality {
		if L.TrackedLocality[i] == zone {
			break
		}
	}

	L.TrackedLocality = append(L.TrackedLocality, zone)
}

func (L *ListenerConfig) GetPriorityTracker() string {
	if L.IsTrackedBy(LocalZone) {
		return LocalZone
	}

	return ""
}

func (L *ListenerConfig) GetTrackerCount() int {
	return len(L.TrackedLocality)
}

func (L *ListenerConfig) CanBeRemovedBy(zone string) bool {
	// NOT vvv
	// 1. there's a priority tracker we're not the owner => no touchy
	// or
	// 2. there's another priority tracker => no touchy
	return !(L.GetPriorityTracker() != "" && L.GetPriorityTracker() != zone)
}

func (L *ListenerConfig) SafeRemove(vhost, route, clusterName, zone string) {
	L.RemoveTracker(zone)

	if L.FindVHost(vhost) == nil {
		return
	}

	L.FindVHost(vhost).SafeRemoveRoute(route, clusterName)
	if len(L.FindVHost(vhost).Routes) == 0 {
		L.RemoveVHost(vhost)
	}

}

func (C *ClusterConfig) Free() {
	for i := range C.EndpointConfig.Endpoints {
		C.EndpointConfig.Endpoints[i] = nil
	}

	C.EndpointConfig.Endpoints = nil
}

func (C *ClusterConfig) IsLocalCluster() bool {
	return C.EndpointConfig.Locality.Zone == LocalZone
}

func (C *ClusterConfig) HasEndpoints() bool {
	return !(C.EndpointConfig.Endpoints == nil || len(C.EndpointConfig.Endpoints) == 0)
}

func GenerateUniqueEndpointName(priority int, namespace, name string) string {
	return fmt.Sprintf("%d-%s-%s", priority, namespace, name)
}

func GenerateUniqConfigName(namespace, name string) string {
	return fmt.Sprintf("%s-%s", namespace, name)
}

func (E *EndpointConfig) Clear() {
	E.Endpoints = nil
}

func (E *EndpointConfig) AddEndpoint(host string, weight int, healthy bool) {
	E.RemoveEndpoint(host) // force remove if it exists to avoid duplicating
	if E.Endpoints == nil {
		E.Endpoints = []*Endpoint{}
	}
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
		logrus.Debugf("Removing endpoint %d (host: %s)", removalI, host)
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

func GenerateListenerName(listen string, xtype ProxyType) string {
	return strings.Replace(listen, ":", "-", -1) + "_" + fmt.Sprintf("%d", xtype)
}

func GenerateVHostName(domains []string) string {
	return strings.Join(domains, "-")
}

func GenerateRouteName(pathSpec string) (string, string) {
	path := "/"
	if pathSpec != "" {
		path = pathSpec
	}
	if path[0:1] != "/" {
		path = "/" + path
	}

	return strings.Replace(path, "/", "_", -1), path
}

type Locality struct {
	// Region string // region is fixed, we only change zone
	Zone string
}
