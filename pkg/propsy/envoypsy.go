package propsy

import (
	"flag"
	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/gogo/protobuf/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"time"
)

var snapshotCache cache.SnapshotCache
var server xds.Server
var grpcServer *grpc.Server

// Hasher returns node ID as an ID
type Hasher struct {
}

// ID function
func (h Hasher) ID(node *core.Node) string {
	if node == nil {
		return "unknown"
	}
	return node.Id
}

var LocalZone string

func init() {
	snapshotCache = cache.NewSnapshotCache(false, Hasher{}, nil)
	server = xds.NewServer(snapshotCache, nil)
	grpcServer = grpc.NewServer()
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	api.RegisterEndpointDiscoveryServiceServer(grpcServer, server)
	api.RegisterClusterDiscoveryServiceServer(grpcServer, server)
	api.RegisterRouteDiscoveryServiceServer(grpcServer, server)
	api.RegisterListenerDiscoveryServiceServer(grpcServer, server)

	reflection.Register(grpcServer)

	flag.StringVar(&LocalZone, "zone", "", "Local zone")
}

func GetGRPCServer() *grpc.Server {
	return grpcServer
}

func UInt32FromInteger(val int) *types.UInt32Value {
	return &types.UInt32Value{
		Value: uint32(val),
	}
}

func UInt64FromInteger(val int) *types.UInt64Value {
	return &types.UInt64Value{
		Value: uint64(val),
	}
}

func GenerateEnvoyConfig(n *NodeConfig) {
	//sendRoutes := []cache.Resource{}
	var sendEndpoints []cache.Resource
	var sendClusters []cache.Resource
	var sendListeners []cache.Resource

	for l := range n.Listeners {
		_listener := n.Listeners[l]
		var vhosts []route.VirtualHost
		for v := range n.Listeners[l].VirtualHosts {
			_vhost := _listener.VirtualHosts[v]
			var routes []route.Route
			for r := range _vhost.Routes {
				_route := _vhost.Routes[r]
				var routedClusters []*route.WeightedCluster_ClusterWeight

				totalWeight, localZoneWeight, otherZoneWeight, canariesWeight, connectTimeout := _route.CalculateWeights()

				logrus.Debugf("total: %d, local: %d, other: %d, clusters: %d", totalWeight, localZoneWeight, otherZoneWeight, len(_route.Clusters))
				for i := range _route.Clusters {
					logrus.Debugf("%d: %s", i, _route.Clusters[i].Name)
				}

				// first setup local-zone cluster
				endpointsAll := _route.GeneratePrioritizedEndpoints(LocalZone)

				addEndpoints := endpointsAll.ToEnvoy(_vhost.Name)
				cluster := ClusterToEnvoy(_vhost.Name, connectTimeout)
				routedCluster := WeightedClusterToEnvoy(_vhost.Name, localZoneWeight)

				sendClusters = append(sendClusters, cluster)
				routedClusters = append(routedClusters, routedCluster)
				sendEndpoints = append(sendEndpoints, addEndpoints)

				// now the others
				for c := range _route.Clusters {
					_cluster := _route.Clusters[c]
					logrus.Debugf("Adding cluster to the cluster set: %s, %b, %s == %s", _cluster.Name, _cluster.IsCanary, _cluster.EndpointConfig.Locality.Zone, LocalZone)
					if _cluster.IsCanary && _cluster.EndpointConfig.Locality.Zone != LocalZone {
						logrus.Debugf(".. Skipping!")
						continue // skip canaries of other zones
					}
					if !_cluster.IsCanary && _cluster.EndpointConfig.Locality.Zone == LocalZone {
						logrus.Debugf("... Skipping too!")
						continue // skip local zones
					}

					weight := otherZoneWeight
					if _cluster.IsCanary {
						weight = canariesWeight
					}

					localityEndpoints := ClusterLoadAssignment{_cluster.EndpointConfig.ToEnvoy(0, 1)}

					addEndpoints := localityEndpoints.ToEnvoy(_cluster.Name)
					cluster := _cluster.ToEnvoy()
					routedCluster := WeightedClusterToEnvoy(_cluster.Name, weight)

					sendClusters = append(sendClusters, cluster)
					routedClusters = append(routedClusters, routedCluster)
					sendEndpoints = append(sendEndpoints, addEndpoints)
				}
				routes = append(routes, _route.ToEnvoy(routedClusters))

			}
			vhost := _vhost.ToEnvoy(routes)
			vhosts = append(vhosts, vhost)
		}

		addListener, err := _listener.ToEnvoy(vhosts)
		if err != nil {
			logrus.Warnf("Error generating listener: ", err.Error())
			continue
		}

		sendListeners = append(sendListeners, addListener)
	}

	logrus.Infof("Generated listeners: %+v", sendListeners)
	logrus.Infof("Generated endpoints: %+v", sendEndpoints)
	logrus.Infof("Generated clusters: %+v", sendClusters)
	logrus.Infof("Setting config for %s", n.NodeName)
	snapshot := cache.NewSnapshot(time.Now().String(), sendEndpoints, sendClusters, nil, sendListeners)
	_ = snapshotCache.SetSnapshot(n.NodeName, snapshot)
}


func RemoveFromEnvoy(node *NodeConfig) {
	snapshotCache.ClearSnapshot(node.NodeName)
}
