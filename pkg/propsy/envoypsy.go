package propsy

import (
	"crypto/tls"
	"crypto/x509"
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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"io/ioutil"
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
var tlsVerifyCA string
var tlsKey string
var tlsCert string
var tlsSkipCN bool

func init() {
	flag.StringVar(&LocalZone, "zone", "", "Local zone")
	flag.StringVar(&tlsVerifyCA, "clientverifyca", "", "Verify CA")
	flag.StringVar(&tlsCert, "servercert", "", "Server TLS Certificate")
	flag.StringVar(&tlsKey, "serverkey", "", "Server TLS key")
	flag.BoolVar(&tlsSkipCN, "peerskipcn", false, "Skip CN verify for peer certificate")
}

func InitGRPCServer() {
	validator := &EnvoyCertificateValidator{}
	if grpcServer == nil {
		if tlsVerifyCA != "" && tlsKey != "" && tlsCert != "" {
			logrus.Info("Setting up TLS")
			certificate, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
			if err != nil {
				logrus.Panicf("Error creating a certificate: %s", err.Error())
			}

			certPool := x509.NewCertPool()
			ca, err := ioutil.ReadFile(tlsVerifyCA)
			if err != nil {
				logrus.Panicf("Error reading a client TLS: %s", err.Error())
			}

			if ok := certPool.AppendCertsFromPEM(ca); !ok {
				logrus.Panic("Error adding a client TLS CA!")
			}

			creds := credentials.NewTLS(&tls.Config{
				ClientAuth:   tls.RequireAndVerifyClientCert,
				Certificates: []tls.Certificate{certificate},
				ClientCAs:    certPool,
			})
			grpcServer = grpc.NewServer(grpc.Creds(creds))
			validator.VerifyCN = !tlsSkipCN
		} else {
			grpcServer = grpc.NewServer()
		}

		snapshotCache = cache.NewSnapshotCache(false, Hasher{}, nil)
		server = xds.NewServer(snapshotCache, PropsyCallbacks{cache: validator})
		discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
		api.RegisterEndpointDiscoveryServiceServer(grpcServer, server)
		api.RegisterClusterDiscoveryServiceServer(grpcServer, server)
		api.RegisterRouteDiscoveryServiceServer(grpcServer, server)
		api.RegisterListenerDiscoveryServiceServer(grpcServer, server)

		reflection.Register(grpcServer)
		logrus.Info("XDS registered")
	}
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

				totalWeight, localZoneWeight, otherZoneWeight, canariesWeight, connectTimeout, maxRequests, lowestPriority, lowestPriorityCanary := _route.CalculateWeights()

				logrus.Debugf("total: %d, local: %d, other: %d, clusters: %d", totalWeight, localZoneWeight, otherZoneWeight, len(_route.Clusters))
				for i := range _route.Clusters {
					logrus.Debugf("%d: %s", i, _route.Clusters[i].Name)
				}

				// first setup local-zone cluster
				endpointsAll := _route.GeneratePrioritizedEndpoints(LocalZone)
				localCluster := _route.GetLowestPriorityCluster()

				addEndpoints := endpointsAll.ToEnvoy(_listener.Name + "_" + _route.GenerateUniqueRouteName())
				cluster := ClusterToEnvoy(_listener.Name+"_"+_route.GenerateUniqueRouteName(), connectTimeout, maxRequests, localCluster.HealthCheck)
				routedCluster := WeightedClusterToEnvoy(_listener.Name+"_"+_route.GenerateUniqueRouteName(), localZoneWeight)

				sendClusters = append(sendClusters, cluster)
				routedClusters = append(routedClusters, routedCluster)
				sendEndpoints = append(sendEndpoints, addEndpoints)

				// now the others
				for c := range _route.Clusters {
					_cluster := _route.Clusters[c]
					logrus.Debugf("Adding cluster to the cluster set: %s, canary %t, %s", _cluster.Name, _cluster.IsCanary, LocalZone)
					if _cluster.IsCanary && lowestPriorityCanary != _cluster.Priority {
						logrus.Debugf(".. Skipping!")
						continue // skip canaries of other zones
					}
					if !_cluster.IsCanary && lowestPriority == _cluster.Priority {
						logrus.Debugf("... Skipping too!")
						continue // skip local zones
					}

					weight := otherZoneWeight
					if _cluster.IsCanary {
						weight = canariesWeight
					}

					localityEndpoints := ClusterLoadAssignment{_cluster.EndpointConfig.ToEnvoy(0, 1)}

					addEndpoints := localityEndpoints.ToEnvoy(_cluster.Name)
					cluster := ClusterToEnvoy(_cluster.Name, _cluster.ConnectTimeout, _cluster.MaxRequests, localCluster.HealthCheck)

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
			logrus.Warnf("Error generating listener: %s", err.Error())
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
