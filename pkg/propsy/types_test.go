package propsy

import (
	"log"
	"testing"
)

func TestNodeConfig(T *testing.T) {
	locality := &Locality{Zone: "test"}
	node := NodeConfig{NodeName: "foobar"}
	node.AddListener(&ListenerConfig{Name: "foobar"})
	node.FindListener("foobar").VirtualHosts = append(node.FindListener("foobar").VirtualHosts,
		&VirtualHost{Name: "foobar"})
	node.FindListener("foobar").FindVHost("foobar").AddRoute(&RouteConfig{Name: "foobar"})
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddCluster(&ClusterConfig{Name: "foobar", Weight: 5, ConnectTimeout: 1, EndpointConfig: &EndpointConfig{
		Name:        "test",
		ServicePort: 123,
		Endpoints:   map[*Locality][]*Endpoint{},
	}})

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint(locality, "1.2.3.4", 10)
	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint(locality, "5.6.7.8", 11)

	if len(node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.Endpoints[locality]) != 2 {
		log.Fatalf("There is a wrong number of endpoints!")
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.RemoveEndpoint(locality, "5.6.7.8")
	if len(node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.Endpoints[locality]) != 1 {
		log.Fatalf("There is a wrong number of endpoints!")
	}

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").AddCluster(&ClusterConfig{Name: "testbar", Weight: 10})

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").TotalWeight() != 15 {
		log.Fatalf("Error total weight: expected 15!")
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
		Endpoints:   map[*Locality][]*Endpoint{},
	}})

	node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.AddEndpoint(locality, "1.2.3.4", 10)

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.GetEndpoint(locality, "1.2.3.4") == nil {
		log.Fatalf("Couldn't find endpoint!")
	}

	if node.FindListener("foobar").FindVHost("foobar").FindRoute("foobar").FindCluster("foobar").EndpointConfig.GetEndpoint(locality, "2.3.4.5") != nil {
		log.Fatalf("Found a non-existing endpoint!")
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

func TestUnique(T *testing.T) {
	if GenerateUniqueEndpointName("namespace", "test") != "namespace-test" {
		log.Fatalf("Wrong uniquely generated endpoint name!")
	}
}
