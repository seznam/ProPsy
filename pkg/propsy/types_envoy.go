package propsy

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
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
