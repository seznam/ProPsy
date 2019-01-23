package propsy

import (
	v22 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"log"
	"testing"
)

func TestHCM(T *testing.T) {
	listener := ListenerConfig{Listen:"8080", Name:"foobar"}
	hcm := listener.GenerateHCM(nil)

	_hcm := &v2.HttpConnectionManager{
		CodecType:  v2.AUTO,
		StatPrefix: "foobar",
		RouteSpecifier: &v2.HttpConnectionManager_RouteConfig{
			RouteConfig: &v22.RouteConfiguration{
				Name:         "foobar",
				VirtualHosts: nil,
			},
		},
		HttpFilters: []*v2.HttpFilter{
			{
				Name: "envoy.router",
				ConfigType: &v2.HttpFilter_Config{
					Config: nil,
				},
			},
		},
	}

	_hcmStruct, _ := util.MessageToStruct(_hcm)

	if !proto.Equal(hcm, _hcm) {
		log.Fatalf("HttpConnectionManager does not match: %+v vs %+v", hcm, _hcm)
	}

	listenerEnvoy, _ := listener.ToEnvoy(nil)
	_listenerEnvoy := &v22.Listener{
		Name: "foobar",
		Address: core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address:    "0.0.0.0",
					Ipv4Compat: true,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: uint32(8080),
					},
				},
			},
		},
		FilterChains: []listener2.FilterChain{{
			Filters: []listener2.Filter{{
				Name: util.HTTPConnectionManager,
				ConfigType: &listener2.Filter_Config{
					Config: _hcmStruct,
				},
			}},
		}},
	}

	if !proto.Equal(listenerEnvoy, _listenerEnvoy) {
		log.Fatalf("Listener does not match: %+v vs %+v", listenerEnvoy, _listenerEnvoy)
	}
}