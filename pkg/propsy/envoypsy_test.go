package propsy

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/proto"
	"log"
	"testing"
	"time"
)

func Test_generateClusterParts(T *testing.T) {
	node := generateSampleNode()
	clusterName := "foobar"
	connectTimeout := 5000
	zoneWeight := 5

	lbEndpoints := ClusterLoadAssignment{
		node.Listeners[0].VirtualHosts[0].Routes[0].Clusters[1].EndpointConfig.ToEnvoy(5, 10),
	}

	cla := lbEndpoints.ToEnvoy(clusterName)

	_cluster := &v2.Cluster{
		Name:           clusterName,
		ConnectTimeout: time.Duration(connectTimeout) * time.Millisecond,
		Type:           v2.Cluster_EDS,
		EdsClusterConfig: &api.Cluster_EdsClusterConfig{
			ServiceName: clusterName,
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
		CommonLbConfig: &api.Cluster_CommonLbConfig{
			LocalityConfigSpecifier: &api.Cluster_CommonLbConfig_ZoneAwareLbConfig_{
				ZoneAwareLbConfig: &api.Cluster_CommonLbConfig_ZoneAwareLbConfig{
					MinClusterSize: UInt64FromInteger(1), // TODO
				},
			},
		},
	}

	_routedCluster := &route.WeightedCluster_ClusterWeight{
		Weight: UInt32FromInteger(zoneWeight),
		Name:   clusterName,
	}

	_cla := &v2.ClusterLoadAssignment{
		Endpoints:   lbEndpoints,
		ClusterName: clusterName,
	}

	cluster := node.Listeners[0].VirtualHosts[0].Routes[0].Clusters[1].ToEnvoy()
	routedCluster := WeightedClusterToEnvoy(clusterName, zoneWeight)

	if !proto.Equal(_cluster, cluster) {
		log.Fatalf("Error generating cluster config: \n%+v\n vs \n%+v", cluster, _cluster)
	}
	if !proto.Equal(_routedCluster, routedCluster) {
		log.Fatalf("Error generating route config: \n%+v\n vs \n%+v", routedCluster, _routedCluster)
	}
	if !proto.Equal(_cla, cla) {
		log.Fatalf("Error generating cluster load assignment: \n%+v\n vs \n%+v", cla, _cla)
	}
}
