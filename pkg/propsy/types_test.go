package propsy

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
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

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddCluster(&ClusterConfig{Name: "testbar", Weight: 10})

	LocalZone = "test"
	if totalWeight, _, _, _, _, _ := node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").CalculateWeights(); totalWeight != 105 {
		log.Fatalf("Error total weight: expected 16, got %d!", totalWeight)
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
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddCluster(&ClusterConfig{Name: "foobar", Weight: 5, ConnectTimeout: 1, EndpointConfig: &EndpointConfig{
		Name:        "test",
		ServicePort: 123,
		Endpoints:   []*Endpoint{},
		Locality: &Locality{
			Zone: "test",
		},
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
		Locality: &core.Locality{
			Zone:    "test",
			SubZone: "admins5",
			Region:  "Seznam",
		},
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
		log.Fatalf("Generated wrong envoy lbendpoint! %+v vs %+v", epc, epc_orig)
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
		AddCluster(&ClusterConfig{Name: "foobar-foreign", Weight: 5, ConnectTimeout: 1000, EndpointConfig: &EndpointConfig{
			Name:        "test",
			ServicePort: 123,
			Endpoints:   []*Endpoint{},
			Locality: &Locality{
				Zone: "test-foreign",
			},
		}})
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddClusters([]*ClusterConfig{
		{
			IsCanary:       true,
			Weight:         5,
			Name:           "foobar",
			ConnectTimeout: 5000,
			EndpointConfig: &EndpointConfig{
				Name: "foobars",
				Locality: &Locality{
					Zone: "test",
				},
				ServicePort: 456,
				Endpoints: []*Endpoint{
					{
						Weight:  5,
						Healthy: true,
						Host:    "4.5.6.7",
					},
				},
			},
		},
		{
			Weight:         1,
			IsCanary:       false,
			Name:           "test-notcanary",
			ConnectTimeout: 2000,
			EndpointConfig: &EndpointConfig{
				Name:        "test-notcanary",
				ServicePort: 9999,
				Locality: &Locality{
					Zone: "test",
				},
			},
		},
	})
	return node
}

func TestUnique(T *testing.T) {
	locality := &Locality{Zone: "test"}

	if GenerateUniqueEndpointName(locality, "namespace", "test") != "test-namespace-test" {
		log.Fatalf("Wrong uniquely generated endpoint name!")
	}
}

func failAinsteadofB(item string, A, B interface{}) {
	log.Fatalf("Wrong %s, got %+v instead of %+v", item, A, B)
}

func assertString(a, b string) {
	if a != b {
		failAinsteadofB("string", a, b)
	}
}

func assertInt64(a, b int64) {
	if a != b {
		failAinsteadofB("int", a, b)
	}
}

func testListenerPrivate(listen, host string, port int64) {
	listener := ListenerConfig{Listen: listen}
	a, b := listener.GenerateListenParts()
	assertString(a, host)
	assertInt64(b, port)
}

func TestListeners(T *testing.T) {
	testListenerPrivate("0:8080", "0.0.0.0", 8080)
	testListenerPrivate("6666", "0.0.0.0", 6666)
	testListenerPrivate("1.2.2.1:8888", "1.2.2.1", 8888)
}
