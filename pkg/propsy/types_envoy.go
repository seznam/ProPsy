package propsy

import (
	"errors"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v22 "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	v23 "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"
	"github.com/sirupsen/logrus"
	"math"
	"time"
)

func (E *Endpoint) ToEnvoy(port int) *endpoint.LbEndpoint {
	healthStatus := core.HealthStatus_HEALTHY
	if !E.Healthy {
		healthStatus = core.HealthStatus_UNHEALTHY
	}
	return &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
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
		},

		HealthStatus: healthStatus,
	}
}

func (E *EndpointConfig) ToEnvoy(priority, weight int) *endpoint.LocalityLbEndpoints {
	var endpoints []*endpoint.LbEndpoint
	for i := range E.Endpoints {
		endpoints = append(endpoints, E.Endpoints[i].ToEnvoy(E.ServicePort))
	}
	return &endpoint.LocalityLbEndpoints{
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

func (C *ClusterConfig) ToEnvoy() *v2.Cluster {
	return ClusterToEnvoy(C.Name, C.ConnectTimeout, C.MaxRequests, C.HealthCheck, C.Outlier)
}

func (V *VirtualHost) ToEnvoy(routes []*route.Route) *route.VirtualHost {
	return &route.VirtualHost{
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

func (L *ListenerConfig) GenerateHCM(vhosts []*route.VirtualHost) *v22.HttpConnectionManager {
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

func (L *ListenerConfig) GenerateWeightedCluster(host *route.VirtualHost) *v23.TcpProxy_WeightedClusters {
	return &v23.TcpProxy_WeightedClusters{
		WeightedClusters: &v23.TcpProxy_WeightedCluster{
			Clusters: []*v23.TcpProxy_WeightedCluster_ClusterWeight{
				{
					Name:   host.Name,
					Weight: uint32(1), // TODO
				},
			},
		},
	}
}

func (L *ListenerConfig) GenerateTCP(clusters *v23.TcpProxy_WeightedClusters) *v23.TcpProxy {
	return &v23.TcpProxy{
		StatPrefix:       L.Name,
		ClusterSpecifier: clusters,
	}
}

type ClusterLoadAssignment []*endpoint.LocalityLbEndpoints

func (C *ClusterLoadAssignment) ToEnvoy(clusterName string) *v2.ClusterLoadAssignment {
	return &v2.ClusterLoadAssignment{
		Endpoints:   *C,
		ClusterName: clusterName,
	}
}

func (R *RouteConfig) GeneratePrioritizedEndpoints(localZone string) ClusterLoadAssignment {
	var endpoints []*endpoint.LocalityLbEndpoints

	// find the lowest local priority
	lowestPriority := math.MaxInt32
	for c := range R.Clusters {
		if R.Clusters[c].Priority < lowestPriority && !R.Clusters[c].IsCanary && R.Clusters[c].EndpointConfig.Endpoints != nil && R.Clusters[c].IsLocalCluster() {
			lowestPriority = R.Clusters[c].Priority
		}
	}

	for c := range R.Clusters {
		_cluster := R.Clusters[c]
		// skip canaries for this
		if _cluster.IsCanary {
			continue
		}

		if _cluster.Priority != lowestPriority &&
			(_cluster.EndpointConfig.Endpoints == nil || len(_cluster.EndpointConfig.Endpoints) == 0) {
			continue // skip non-lowest priority other clusters, they just clobber the output
		}

		priority := 1
		if _cluster.Priority == lowestPriority {
			priority = 0 // local cluster gets priority 0
		}

		endpoints = append(endpoints, _cluster.EndpointConfig.ToEnvoy(priority, 1)) // todo weight?
	}

	return endpoints
}

func (H *HealthCheckConfig) ToEnvoy() *core.HealthCheck {
	if H == nil {
		return nil
	}

	hc := &core.HealthCheck{
		Interval: &H.Interval,
		Timeout:  &H.Timeout,
		ReuseConnection: &types.BoolValue{
			Value: H.ReuseConnection,
		},
		HealthyThreshold:   UInt32FromInteger(H.HealthyTreshold),
		UnhealthyThreshold: UInt32FromInteger(H.UnhealthyTreshold),
	}

	switch H.HealthChecker {
	case HTTPHealthCheck, HTTP2HealthCheck:
		hc.HealthChecker = &core.HealthCheck_HttpHealthCheck_{
			HttpHealthCheck: &core.HealthCheck_HttpHealthCheck{
				Path:     H.HTTPPath,
				Host:     H.HTTPHost,
				UseHttp2: H.HealthChecker == HTTP2HealthCheck,
			},
		}
	case GRPCHealthCheck:
		hc.HealthChecker = &core.HealthCheck_GrpcHealthCheck_{
			GrpcHealthCheck: &core.HealthCheck_GrpcHealthCheck{},
		}
	case TCPHealthCheck:
		hc.HealthChecker = &core.HealthCheck_TcpHealthCheck_{
			TcpHealthCheck: &core.HealthCheck_TcpHealthCheck{},
		}
	default:
		return nil
	}

	return hc
}

func (O *OutlierConfig) ToEnvoy() *cluster.OutlierDetection {
	if O == nil {
		return nil
	}

	return &cluster.OutlierDetection{
		Interval:                  DurationToDuration(O.Interval),
		BaseEjectionTime:          DurationToDuration(O.EjectionTime),
		Consecutive_5Xx:           UInt32FromInteger(O.ConsecutiveErrors),
		ConsecutiveGatewayFailure: UInt32FromInteger(O.ConsecutiveGwErrors),
		MaxEjectionPercent:        UInt32FromInteger(O.EjectionPercent),
		SuccessRateMinimumHosts:   UInt32FromInteger(O.MinimumHosts),
		SuccessRateRequestVolume:  UInt32FromInteger(O.MinimumRequests),
	}
}

func ClusterToEnvoy(targetName string, connectTimeout, maxRequests int, healthCheck *HealthCheckConfig, outlier *OutlierConfig) *v2.Cluster {
	maxRequestsPtr := UInt32FromInteger(maxRequests)
	if maxRequests == 0 {
		maxRequestsPtr = nil
	}

	hc := healthCheck.ToEnvoy()
	var hcs []*core.HealthCheck
	if hc != nil { // only assign when hc exists, having a null item is bad
		hcs = append(hcs, hc)
	}

	connectTimeoutDuration := time.Duration(connectTimeout) * time.Millisecond

	return &v2.Cluster{
		Name:           targetName,
		ConnectTimeout: &connectTimeoutDuration,
		ClusterDiscoveryType: &v2.Cluster_Type{
			Type: v2.Cluster_EDS,
		},
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
		MaxRequestsPerConnection: maxRequestsPtr,
		HealthChecks:             hcs,
		OutlierDetection:         outlier.ToEnvoy(),
	}
}

func (L *ListenerConfig) ToEnvoy(vhosts []*route.VirtualHost) (*v2.Listener, error) {
	listenHost, listenPort := L.GenerateListenParts()

	var FilterConfig *types.Struct

	var FilterType string
	var err error

	logrus.Debugf("Generating listener for type: %s", string(L.Type))

	switch L.Type {
	case HTTP:
		FilterType = util.HTTPConnectionManager
		FilterConfig, err = util.MessageToStruct(L.GenerateHCM(vhosts))
	case TCP:
		if len(vhosts) != 1 {
			return nil, errors.New("there are too many or no vhosts to this listener")
		}
		FilterType = util.TCPProxy
		FilterConfig, err = util.MessageToStruct(L.GenerateTCP(L.GenerateWeightedCluster(vhosts[0])))
	}

	if err != nil {
		return nil, err
	}

	var tlsContext *auth.DownstreamTlsContext = nil
	if L.TLSSecret != nil {
		if len(L.TLSSecret.Key) == 0 || len(L.TLSSecret.Certificate) == 0 {
			logrus.Warnf("There is no TLS data for %s" + L.Name)
		} else {
			tlsContext = &auth.DownstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{
					TlsCertificates: []*auth.TlsCertificate{{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_InlineBytes{
								InlineBytes: L.TLSSecret.Certificate,
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_InlineBytes{
								InlineBytes: L.TLSSecret.Key,
							},
						},
					}},
					/*ValidationContextType: &auth.CommonTlsContext_ValidationContext{
						ValidationContext: &auth.CertificateValidationContext{
							TrustedCa: &core.DataSource{
								Specifier: &core.DataSource_InlineBytes{
									InlineBytes: L.TLSSecret.CA,
								},
							},
						},
					},*/
				},
			}
		}
	}

	return &v2.Listener{
		Name: L.Name,
		Address: &core.Address{
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
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name: FilterType,
				ConfigType: &listener.Filter_Config{
					Config: FilterConfig,
				},
			}},
			TlsContext: tlsContext,
		}},
	}, nil
}

func (R *RouteConfig) ToEnvoy(routedClusters []*route.WeightedCluster_ClusterWeight) *route.Route {
	totalWeight, _, _, _, _, _ := R.CalculateWeights()

	return &route.Route{
		Match: &route.RouteMatch{
			PathSpecifier: &route.RouteMatch_Prefix{
				Prefix: R.PathPrefix,
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
				PrefixRewrite: R.PrefixRewrite,
				Timeout:       &R.Timeout,
			},
		},
	}
}
