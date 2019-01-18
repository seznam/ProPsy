package propsy

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v22 "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"time"
)

func (E *Endpoint) ToEnvoy(port int) endpoint.LbEndpoint {
	healthStatus := core.HealthStatus_HEALTHY
	if ! E.Healthy {
		healthStatus = core.HealthStatus_UNHEALTHY
	}
	return endpoint.LbEndpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: E.Host,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: uint32(port),
						},
					},
				},
			},
		},
		HealthStatus: healthStatus,
	}
}

func (E *EndpointConfig) ToEnvoy(priority, weight int) endpoint.LocalityLbEndpoints {
	endpoints := []endpoint.LbEndpoint{}
	for i := range E.Endpoints {
		endpoints = append(endpoints, E.Endpoints[i].ToEnvoy(E.ServicePort))
	}
	return endpoint.LocalityLbEndpoints{
		Locality:            E.Locality.ToEnvoy(),
		LbEndpoints:         endpoints,
		Priority:            uint32(priority),
		LoadBalancingWeight: UInt32FromInteger(weight),
	}
}

func (L *Locality) ToEnvoy() *core.Locality {
	return &core.Locality{
		Zone:    L.Zone,
		Region:  "Seznam",
		SubZone: "admins5",
	}
}

func (C *ClusterConfig) ToEnvoy(targetName string) *v2.Cluster {
	return ClusterToEnvoy(targetName, C.ConnectTimeout)
}

func (V *VirtualHost) ToEnvoy(routes []route.Route) route.VirtualHost {
	return route.VirtualHost{
		Name:    V.Name,
		Domains: V.Domains,
		Routes:  routes,
	}
}

func WeightedClusterToEnvoy(clusterName string, zoneWeight int) *route.WeightedCluster_ClusterWeight {
	return &route.WeightedCluster_ClusterWeight{
		Weight: UInt32FromInteger(zoneWeight),
		Name:   clusterName,
	}
}

func (L *ListenerConfig) GenerateHCM(vhosts []route.VirtualHost) *v22.HttpConnectionManager {
	return &v22.HttpConnectionManager{
		CodecType:  v22.AUTO,
		StatPrefix: L.Name,
		RouteSpecifier: &v22.HttpConnectionManager_RouteConfig{
			RouteConfig: &v2.RouteConfiguration{
				Name:         L.Name,
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
}

type ClusterLoadAssignment []endpoint.LocalityLbEndpoints

func (C *ClusterLoadAssignment) ToEnvoy(clusterName string) *v2.ClusterLoadAssignment{
	return &v2.ClusterLoadAssignment{
		Endpoints:   *C,
		ClusterName: clusterName,
	}
}

func (R *RouteConfig) GeneratePrioritizedEndpoints(localZone string) ClusterLoadAssignment {
	endpoints := []endpoint.LocalityLbEndpoints{}
	for c := range R.Clusters {
		_cluster := R.Clusters[c]
		// skip canaries for this
		if _cluster.IsCanary {
			continue
		}

		priority := 1
		if _cluster.EndpointConfig.Locality.Zone == localZone {
			priority = 0 // local cluster gets priority 0
		}

		endpoints = append(endpoints, _cluster.EndpointConfig.ToEnvoy(priority, 1)) // todo weight?
	}

	return endpoints
}

func ClusterToEnvoy(targetName string, connectTimeout int) *v2.Cluster {
	return &v2.Cluster{
		Name:           targetName,
		ConnectTimeout: time.Duration(connectTimeout) * time.Millisecond,
		Type:           v2.Cluster_EDS,
		EdsClusterConfig: &v2.Cluster_EdsClusterConfig{
			ServiceName: targetName,
			EdsConfig: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
					ApiConfigSource: &core.ApiConfigSource{
						ApiType: core.ApiConfigSource_GRPC,
						GrpcServices: []*core.GrpcService{{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
									ClusterName: "xds_cluster", // todo decide how this gets discovered
								},
							},
						}},
					},
				},
			},
		},
		CommonLbConfig: &v2.Cluster_CommonLbConfig{
			LocalityConfigSpecifier: &v2.Cluster_CommonLbConfig_ZoneAwareLbConfig_{
				ZoneAwareLbConfig: &v2.Cluster_CommonLbConfig_ZoneAwareLbConfig{
					MinClusterSize: UInt64FromInteger(1), // TODO
				},
			},
		},
	}
}

func (L *ListenerConfig) ToEnvoy(vhosts []route.VirtualHost) (*v2.Listener, error) {
	listenHost, listenPort := L.GenerateListenParts()

	hcm, err := util.MessageToStruct(L.GenerateHCM(vhosts))
	if err != nil {
		return nil, err
	}

	return &v2.Listener{
		Name: L.Name,
		Address: core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address:    listenHost,
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
	}, nil
}

func (R *RouteConfig) ToEnvoy(routedClusters []*route.WeightedCluster_ClusterWeight) route.Route {
	totalWeight, _, _, _, _ := R.CalculateWeights()

	return route.Route{
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
	}
}