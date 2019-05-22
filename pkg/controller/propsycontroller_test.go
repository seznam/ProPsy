package controller

import (
	"github.com/seznam/ProPsy/pkg/apis/propsy/v1"
	"github.com/seznam/ProPsy/pkg/propsy"
	"github.com/seznam/ProPsy/pkg/testutils"
	"log"
	"reflect"
	"testing"
	"time"
)

var controller1, controller2 ProPsyController
var ppsCache *propsy.ProPsyCache
var zone *propsy.Locality

func init() {
	ppsCache = propsy.NewProPsyCache()
	zone = &propsy.Locality{
		Zone: "left",
	}
	propsy.LocalZone = "left"
	controller1 = ProPsyController{
		locality: zone,
		ppsCache: ppsCache,
	}
	controller2 = ProPsyController{
		locality: &propsy.Locality{Zone: "right"},
		ppsCache: ppsCache,
	}

	propsy.InitGRPCServer() // need this to set up the snapshot cache
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
		TrackedLocality: []string{"left"},
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

	listener := controller1.NewListenerConfig(&pps)

	if !reflect.DeepEqual(*listener, properListener) {
		log.Println("Checked func NewListenerConfig:\n======")
		log.Fatalf("Listeners are not correct:\nGenerated:\n%+v\n== vs ==\nExpected:\n%+v", *listener, properListener)
		log.Println("=====")
	}

	pps.Spec.Type = "TCP"
	if xpps := controller1.NewListenerConfig(&pps); xpps == nil || xpps.Type != propsy.TCP {
		log.Fatalf("Error decoding TCP type service")
	}
	pps.Spec.Type = ""
	if xpps := controller1.NewListenerConfig(&pps); xpps == nil || xpps.Type != propsy.HTTP {
		log.Fatalf("Error decoding TCP type service")
	}
	pps.Spec.Type = "FAIL"
	if xpps := controller1.NewListenerConfig(&pps); xpps != nil {
		log.Fatalf("Error decoding unknown type service")
	}
}

func Test_PriorityTrackers(t *testing.T) {
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

	pps2 := v1.ProPsyService{
		Spec: v1.ProPsyServiceSpec{
			MaxRequestsPerConnection: 3,
			Timeout:                  10,
			PathPrefix:               "/foobar/",
			PrefixRewrite:            "/",
			Service:                  "SomeService",
			CanaryService:            "CanaryService",
			Nodes: []string{
				"node-a",
				"node-c",
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

	pps2changed := v1.ProPsyService{
		Spec: v1.ProPsyServiceSpec{
			MaxRequestsPerConnection: 3,
			Timeout:                  10,
			PathPrefix:               "/foobar/",
			PrefixRewrite:            "/",
			Service:                  "SomeService",
			CanaryService:            "CanaryService",
			Nodes: []string{
				"node-a",
				"node-c",
				"node-d",
			},
			Listen:               "127.0.0.1:4334",
			Type:                 "HTTP",
			ConnectTimeout:       1234,
			ServicePort:          6010,
			Percent:              99,
			CanaryPercent:        1,
			TLSCertificateSecret: "",
		},
	}

	controller1.PPSAdded(&pps)
	controller2.PPSAdded(&pps2)

	testutils.AssertInt(len(ppsCache.GetNodes()), 3)
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[0].GetPriorityTracker(), "left")
	testutils.AssertInt(len(ppsCache.GetNodes()["node-a"].Listeners), 1)
	controller2.PPSRemoved(&pps2, false)
	// TODO this should be valid but due to how things are set up there will be hanging leftovers on `node-c`
	//	testutils.AssertInt(len(ppsCache.GetNodes()), 2)
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[0].GetPriorityTracker(), "left")
	testutils.AssertInt(len(ppsCache.GetNodes()["node-a"].Listeners), 1)
	controller1.PPSRemoved(&pps, false)
	testutils.AssertInt(len(ppsCache.GetNodes()["node-a"].Listeners), 0)

	pps2.Spec.Listen = "127.0.0.1:4321"
	controller2.PPSAdded(&pps2)
	testutils.AssertString(ppsCache.GetNodes()["node-a"].Listeners[0].Listen, "127.0.0.1:4321")
	// TODO no string means that there is no priority tracker and anybody can change it (should be changed to 1st come 1st serve
	// but not until we have tests for this so it doesn't break anything)
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[0].GetPriorityTracker(), "")
	controller1.PPSAdded(&pps)
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[0].GetPriorityTracker(), "")
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[1].GetPriorityTracker(), "left")
	testutils.AssertInt(len(ppsCache.GetNodes()["node-a"].Listeners), 2)
	testutils.AssertString(ppsCache.GetNodes()["node-a"].Listeners[0].Listen, "127.0.0.1:4321") //got added, not replaced
	testutils.AssertString(ppsCache.GetNodes()["node-a"].Listeners[1].Listen, "127.0.0.1:1234") //got added, not replaced

	controller2.PPSChanged(&pps2, &pps2changed)
	// TODO this should add a node, however we are not making a proper diff so it will not add it once there's a tracked locality
	// it may look like there's 4 but only 3 will be listening correctly. Known bug for now...
	testutils.AssertInt(len(ppsCache.GetNodes()), 4) // a b c d
	testutils.AssertInt(len(ppsCache.GetNodes()["node-a"].Listeners), 2)
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[1].Listen, "127.0.0.1:4334")
	testutils.AssertString(ppsCache.GetOrCreateNode("node-a").Listeners[0].Listen, "127.0.0.1:1234")
	controller1.PPSRemoved(&pps, false)
	// TODO no way to reset it to the other locality's before update comes
	testutils.AssertString(ppsCache.GetNodes()["node-a"].Listeners[0].Listen, "127.0.0.1:4334")
}
