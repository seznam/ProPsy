package controller

import (
	"github.com/seznam/ProPsy/pkg/apis/propsy/v1"
	"github.com/seznam/ProPsy/pkg/propsy"
	"log"
	"reflect"
	"testing"
	"time"
)

var controller ProPsyController
var zone *propsy.Locality

func init() {
	zone = &propsy.Locality{
		Zone: "ko",
	}
	propsy.LocalZone = "ko"
	controller = ProPsyController{
		locality: zone,
		ppsCache: propsy.NewProPsyCache(),
	}
}

func Test_NewListenerConfig(t *testing.T) {
	pps := v1.ProPsyService{
		Spec: v1.ProPsyServiceSpec{
			MaxRequestsPerConnection: 3,
			Timeout:                  10,
			PathPrefix:               "/foobar/",
			PrefixRewrite:            "/",
			Service:                  "SomeService",
			CanaryService:            "CanaryService",
			Nodes: []string{
				"node-a",
				"node-b",
			},
			Listen:               "127.0.0.1:1234",
			Type:                 "HTTP",
			ConnectTimeout:       1234,
			ServicePort:          6010,
			Percent:              99,
			CanaryPercent:        1,
			TLSCertificateSecret: "",
		},
	}

	properListener := propsy.ListenerConfig{
		TLSSecret:       nil,
		TrackedLocality: []string{"ko"},
		Type:            propsy.HTTP,
		Name:            "127.0.0.1-1234_0",
		Listen:          "127.0.0.1:1234",
		VirtualHosts: []*propsy.VirtualHost{{
			Name:    "*",
			Domains: []string{"*"},
			Routes: []*propsy.RouteConfig{{
				Name:          "_foobar_",
				PrefixRewrite: "/",
				PathPrefix:    "/foobar/",
				Timeout:       10 * time.Millisecond,
				Clusters:      nil,
			}},
		}},
	}

	listener := controller.NewListenerConfig(&pps)

	if !reflect.DeepEqual(*listener, properListener) {
		log.Println("Checked func NewListenerConfig:\n======")
		log.Fatalf("Listeners are not correct:\nGenerated:\n%+v\n== vs ==\nExpected:\n%+v", *listener, properListener)
		log.Println("=====")
	}

	pps.Spec.Type = "TCP"
	if xpps := controller.NewListenerConfig(&pps); xpps == nil || xpps.Type != propsy.TCP {
		log.Fatalf("Error decoding TCP type service")
	}
	pps.Spec.Type = ""
	if xpps := controller.NewListenerConfig(&pps); xpps == nil || xpps.Type != propsy.HTTP {
		log.Fatalf("Error decoding TCP type service")
	}
	pps.Spec.Type = "FAIL"
	if xpps := controller.NewListenerConfig(&pps); xpps != nil {
		log.Fatalf("Error decoding unknown type service")
	}
}
