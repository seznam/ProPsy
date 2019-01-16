package propsy

import (
	"flag"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v22 "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"strconv"
	"strings"
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
	sendEndpoints := []cache.Resource{}
	sendClusters := []cache.Resource{}
	sendListeners := []cache.Resource{}

	for l := range n.Listeners {
		_listener := n.Listeners[l]
		vhosts := []route.VirtualHost{}
		for v := range n.Listeners[l].VirtualHosts {
			_vhost := _listener.VirtualHosts[v]
			routes := []route.Route{}
			for r := range _vhost.Routes {
				_route := _vhost.Routes[r]
				routedClusters := []*route.WeightedCluster_ClusterWeight{}

				// find the total sum of weights that are not our cluster and our clusters as well
				localZoneWeight := 0
				otherZoneWeight := 0
				canariesWeight := 0
				connectTimeout := 0
				otherZoneCount := 0

				for c := range _route.Clusters {
					_cluster := _route.Clusters[c]
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

				totalWeight := localZoneWeight + otherZoneCount * otherZoneWeight + canariesWeight // canaries are separated
				logrus.Debugf("total: %d, local: %d, other: %d, clusters: %d", totalWeight, localZoneWeight, otherZoneWeight, len(_route.Clusters))
				for i := range _route.Clusters {
					logrus.Debugf("%d: %s", i, _route.Clusters[i].Name)
				}
				// first setup local-zone cluster
				routedClusters = append(routedClusters, &route.WeightedCluster_ClusterWeight{
					Weight: UInt32FromInteger(localZoneWeight),
					Name:   _vhost.Name,
				})

				endpointsAll := []endpoint.LocalityLbEndpoints{}
				for c := range _route.Clusters {
					_locality := _route.Clusters[c].EndpointConfig.Locality

					if _route.Clusters[c].IsCanary {
						continue
					}

					priority := 1
					if _locality.Zone == LocalZone {
						priority = 0
					}
					endpointsAll = append(endpointsAll, _route.Clusters[c].EndpointConfig.ToEnvoy(priority, 1))
				}

				cluster := &v2.Cluster{
					Name:           _vhost.Name,
					ConnectTimeout: time.Duration(connectTimeout) * time.Millisecond,
					Type:           v2.Cluster_STATIC,
					LoadAssignment: &v2.ClusterLoadAssignment{
						ClusterName: _vhost.Name,
						Endpoints:   endpointsAll,
					},
					CommonLbConfig: &api.Cluster_CommonLbConfig{
						LocalityConfigSpecifier: &api.Cluster_CommonLbConfig_ZoneAwareLbConfig_{
							ZoneAwareLbConfig: &api.Cluster_CommonLbConfig_ZoneAwareLbConfig{
								MinClusterSize: UInt64FromInteger(1), // TODO
							},
						},
					},
				}

				sendClusters = append(sendClusters, cluster)

				// now the others
				for c := range _route.Clusters {
					_cluster := _route.Clusters[c]
					logrus.Debugf("Adding cluster to the cluster set: %s, %b, %s == %s", _cluster.Name, _cluster.IsCanary, _cluster.EndpointConfig.Locality.Zone, LocalZone)
					if _cluster.IsCanary && _cluster.EndpointConfig.Locality.Zone != LocalZone  {
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
					routedClusters = append(routedClusters, &route.WeightedCluster_ClusterWeight{
						Name:   _cluster.Name,
						Weight: UInt32FromInteger(weight),
					})

					localityEndpoints := []endpoint.LocalityLbEndpoints{_cluster.EndpointConfig.ToEnvoy(0, 1)}

					cluster := &v2.Cluster{
						Name:           _cluster.Name,
						ConnectTimeout: time.Duration(_cluster.ConnectTimeout) * time.Millisecond,
						Type:           v2.Cluster_STATIC,
						LoadAssignment: &v2.ClusterLoadAssignment{
							ClusterName: _cluster.Name,
							Endpoints:   localityEndpoints,
						},
						CommonLbConfig: &api.Cluster_CommonLbConfig{
							LocalityConfigSpecifier: &api.Cluster_CommonLbConfig_ZoneAwareLbConfig_{
								ZoneAwareLbConfig: &api.Cluster_CommonLbConfig_ZoneAwareLbConfig{
									MinClusterSize: UInt64FromInteger(1), // TODO
								},
							},
						},
					}

					sendClusters = append(sendClusters, cluster)
				}
				routes = append(routes, route.Route{
					Match: route.RouteMatch{
						PathSpecifier: &route.RouteMatch_Prefix{
							Prefix: "/",
						},
					},
					Action: &route.Route_Route{
						Route: &route.RouteAction{
							ClusterSpecifier: &route.RouteAction_WeightedClusters{
								WeightedClusters: &route.WeightedCluster{
									Clusters:    routedClusters,
									TotalWeight: UInt32FromInteger(totalWeight),
								},
							},
						},
					},
				})

			}
			vhost := route.VirtualHost{
				Name:    _vhost.Name,
				Domains: _vhost.Domains,
				Routes:  routes,
			}
			vhosts = append(vhosts, vhost)
		}

		manager := &v22.HttpConnectionManager{
			CodecType:  v22.AUTO,
			StatPrefix: _listener.Name,
			RouteSpecifier: &v22.HttpConnectionManager_RouteConfig{
				RouteConfig: &v2.RouteConfiguration{
					Name:         _listener.Name,
					VirtualHosts: vhosts,
				},
			},
			HttpFilters: []*v22.HttpFilter{
				{
					Name: "envoy.router",
					ConfigType: &v22.HttpFilter_Config{
						Config: nil,
					},
				},
			},
		}

		hcm, err := util.MessageToStruct(manager)
		if err != nil {
			logrus.Warnf("Error formatting message to struct: %s", err.Error())
			return
		}

		parts := strings.Split(_listener.Listen, ":")
		var listenPort int64 = 0
		if len(parts) > 1 {
			listenPort, _ = strconv.ParseInt(parts[1], 10, 32)
		}
		if parts[0] == "" || parts[0] == "0" {
			parts[0] = "0.0.0.0"
		}

		sendListeners = append(sendListeners, &v2.Listener{
			Name: _listener.Name,
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:    parts[0],
						Ipv4Compat: true,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: uint32(listenPort),
						},
					},
				},
			},
			FilterChains: []listener.FilterChain{{
				Filters: []listener.Filter{{
					Name: util.HTTPConnectionManager,
					ConfigType: &listener.Filter_Config{
						Config: hcm,
					},
				}},
			}},
		})
	}

	logrus.Infof("Generated listeners: %+v", sendListeners)
	logrus.Infof("Generated endpoints: %+v", sendEndpoints)
	logrus.Infof("Generated clusters: %+v", sendClusters)
	logrus.Infof("Setting config for %s", n.NodeName)
	snapshot := cache.NewSnapshot(time.Now().String(), nil, sendClusters, nil, sendListeners)
	_ = snapshotCache.SetSnapshot(n.NodeName, snapshot)
}

func RemoveFromEnvoy(node *NodeConfig) {
	snapshotCache.ClearSnapshot(node.NodeName)
}
