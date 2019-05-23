package propsy

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/seznam/ProPsy/pkg/testutils"
	"log"
	"reflect"
	"testing"
)

func TestNodeConfig(T *testing.T) {
	node := generateSampleNode()

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.GetEndpoint("4.5.6.7") == nil {
		log.Fatalf("There is no 4.5.6.7 endpoint!")
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint("1.2.3.4", 10, true)
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint("5.6.7.8", 11, true)

	if len(node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.Endpoints) != 3 {
		log.Fatalf("There is a wrong number of endpoints!")
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.RemoveEndpoint("5.6.7.8")
	if len(node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.Endpoints) != 2 {
		log.Fatalf("There is a wrong number of endpoints!")
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddCluster(&ClusterConfig{Name: "testbar", Weight: 10, Priority: 3, EndpointConfig: &EndpointConfig{Locality: &Locality{Zone: "ko"}}})

	LocalZone = "test"
	if totalWeight, _, _, _, _, _ := node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").CalculateWeights(); totalWeight != 10 {
		log.Fatalf("Error total weight: expected 10, got %d!", totalWeight)
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").RemoveCluster("foobar")
	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar") != nil {
		log.Fatalf("Cluster was not removed!")
	}

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("testbar") == nil {
		log.Fatalf("Cluster was removed!")
	}

	node.FindListener("foobar").FindVHost("foobar").RemoveRoute("foobar")
	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar") != nil {
		log.Fatalf("Route was not removed!")
	}

	node.FindListener("foobar").RemoveVHost("foobar")
	if node.FindListener("foobar").FindVHost("foobar") != nil {
		log.Fatalf("Route was not removed!")
	}

	node.AddListener(&ListenerConfig{Name: "foobar"})
	node.FindListener("foobar").VirtualHosts = append(node.FindListener("foobar").VirtualHosts,
		&VirtualHost{Name: "foobar"})
	node.FindListener("foobar").FindVHost("foobar").AddRoute(&RouteConfig{Name: "foobar"})
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddCluster(&ClusterConfig{Name: "foobar", Weight: 5, Priority: 4, ConnectTimeout: 1, EndpointConfig: &EndpointConfig{
		Name:        "test",
		ServicePort: 123,
		Endpoints:   []*Endpoint{},
	}})

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint("1.2.3.4", 10, true)
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint("11.22.33.44", 20, false)

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.GetEndpoint("1.2.3.4") == nil {
		log.Fatalf("Couldn't find endpoint!")
	}

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.GetEndpoint("2.3.4.5") != nil {
		log.Fatalf("Found a non-existing endpoint!")
	}
	epc := node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.ToEnvoy(2, 3)
	epc_orig := endpoint.LocalityLbEndpoints{
		LoadBalancingWeight: UInt32FromInteger(3),
		Priority:            uint32(2),
		LbEndpoints: []endpoint.LbEndpoint{
			{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									Address: "1.2.3.4",
									PortSpecifier: &core.SocketAddress_PortValue{
										PortValue: uint32(123),
									},
								},
							},
						},
					},
				},
				HealthStatus: core.HealthStatus_HEALTHY,
			},
			{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									Address: "11.22.33.44",
									PortSpecifier: &core.SocketAddress_PortValue{
										PortValue: uint32(123),
									},
								},
							},
						},
					},
				},
				HealthStatus: core.HealthStatus_UNHEALTHY,
			},
		},
	}
	if !reflect.DeepEqual(epc, epc_orig) {
		log.Fatalf("Generated wrong envoy lbendpoint!\n%+v\nvs\n%+v", epc, epc_orig)
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").Free()
	if len(node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.Endpoints) != 0 {
		log.Fatalf("Endpoints were not removed!")
	}

	node.Free()

	if node.FindListener("foobar") != nil {
		log.Fatalf("Listener was not removed!")
	}
}

func generateSampleNode() NodeConfig {
	LocalZone = "test"

	node := NodeConfig{NodeName: "foobar"}
	node.AddListener(&ListenerConfig{Name: "foobar"})
	node.FindListener("foobar").VirtualHosts = append(node.FindListener("foobar").VirtualHosts,
		&VirtualHost{Name: "foobar"})
	node.FindListener("foobar").FindVHost("foobar").AddRoute(&RouteConfig{Name: "foobar"})
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").
		AddCluster(&ClusterConfig{Name: "foobar-foreign", Weight: 5, ConnectTimeout: 1000, Priority: 1, EndpointConfig: &EndpointConfig{
			Name:        "test",
			ServicePort: 123,
			Endpoints:   []*Endpoint{},
			Locality:    &Locality{Zone: "test"},
		}})
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddClusters([]*ClusterConfig{
		{
			IsCanary:       true,
			Weight:         5,
			Name:           "foobar",
			ConnectTimeout: 5000,
			Priority:       0,
			EndpointConfig: &EndpointConfig{
				Name:        "foobars",
				ServicePort: 456,
				Endpoints: []*Endpoint{
					{
						Weight:  5,
						Healthy: true,
						Host:    "4.5.6.7",
					},
				},
				Locality: &Locality{Zone: "test"},
			},
		},
		{
			Weight:         1,
			IsCanary:       false,
			Name:           "test-notcanary",
			ConnectTimeout: 2000,
			Priority:       1,
			EndpointConfig: &EndpointConfig{
				Name:        "test-notcanary",
				ServicePort: 9999,
				Locality:    &Locality{Zone: "test"},
			},
		},
	})
	return node
}

func TestUnique(T *testing.T) {
	if GenerateUniqueEndpointName(1, "namespace", "test") != "1-namespace-test" {
		log.Fatalf("Wrong uniquely generated endpoint name!")
	}
}

func testListenerPrivate(listen, host string, port int64) {
	listener := ListenerConfig{Listen: listen}
	a, b := listener.GenerateListenParts()
	testutils.AssertString(a, host)
	testutils.AssertInt64(b, port)
}

func TestListeners(T *testing.T) {
	testListenerPrivate("0:8080", "0.0.0.0", 8080)
	testListenerPrivate("6666", "0.0.0.0", 6666)
	testListenerPrivate("1.2.2.1:8888", "1.2.2.1", 8888)
}

func TestRouteGenerator(T *testing.T) {
	test, path := GenerateRouteName("foobar/baz")
	testutils.AssertString(test, "_foobar_baz")
	testutils.AssertString(path, "/foobar/baz")
	test, path = GenerateRouteName("")
	testutils.AssertString(test, "_")
	testutils.AssertString(path, "/")
	test, path = GenerateRouteName("/foo")
	testutils.AssertString(test, "_foo")
	testutils.AssertString(path, "/foo")
}

func TestListenerConfig_Trackers(T *testing.T) {
	lis := ListenerConfig{}
	LocalZone = "left"
	lis.AddTracker("left")
	lis.AddTracker("right")

	testutils.AssertString(lis.GetPriorityTracker(), "left")
	lis.AddTracker("left")
	testutils.AssertInt(lis.GetTrackerCount(), 2)
	lis.RemoveTracker("left")
	testutils.AssertInt(lis.GetTrackerCount(), 1)
	// TODO this might change when we have proper lower-level priority tracking for PPS
	testutils.AssertString(lis.GetPriorityTracker(), "")
}