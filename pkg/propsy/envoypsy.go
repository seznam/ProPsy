package propsy

import (
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
}

func GetGRPCServer() *grpc.Server {
	return grpcServer
}

func UInt32FromInteger(val int) *types.UInt32Value {
	return &types.UInt32Value{
		Value: uint32(val),
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
				for c := range _route.Clusters {
					_cluster := _route.Clusters[c]
					routedClusters = append(routedClusters, &route.WeightedCluster_ClusterWeight{
						Name:   _cluster.Name,
						Weight: UInt32FromInteger(_cluster.Weight),
					})

					localityEndpoints := []endpoint.LocalityLbEndpoints{}

					for _locality, _endpoints := range _cluster.EndpointConfig.Endpoints {
						endpoints := []endpoint.LbEndpoint{}
						for i := range _endpoints {
							endpoints = append(endpoints, endpoint.LbEndpoint{
								Endpoint: &endpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address: _endpoints[i].Host,
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: uint32(_cluster.EndpointConfig.ServicePort),
												},
											},
										},
									},
								},
							})
						}
						localityEndpoints = append(localityEndpoints, endpoint.LocalityLbEndpoints{
							Locality: &core.Locality{
								Zone:    _locality.Zone,
								Region:  "Seznam",
								SubZone: "admins5", // todo hahaha?
							},
							LbEndpoints: endpoints,
						})
					}

					cluster := &v2.Cluster{
						Name:           _cluster.Name,
						ConnectTimeout: time.Duration(_cluster.ConnectTimeout) * time.Millisecond,
						Type:           v2.Cluster_STATIC,
						LoadAssignment: &v2.ClusterLoadAssignment{
							ClusterName: _cluster.Name,
							Endpoints:   localityEndpoints,
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
									TotalWeight: UInt32FromInteger(_route.TotalWeight()),
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
	snapshot := cache.NewSnapshot(time.Now().String(), nil, sendClusters, nil, sendListeners)
	_ = snapshotCache.SetSnapshot(n.NodeName, snapshot)
}

func RemoveFromEnvoy(node *NodeConfig) {
	snapshotCache.ClearSnapshot(node.NodeName)
}
