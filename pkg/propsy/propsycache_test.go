package propsy

import (
	"log"
	"testing"
)

var ppscache *ProPsyCache

func init() {
	ppscache = NewProPsyCache()
}

func Test_Nodes(t *testing.T) {
	nodeA := ppscache.GetOrCreateNode("test-node")
	nodeB := ppscache.GetOrCreateNode("test-node")
	if nodeA != nodeB {
		log.Fatal("Nodes do not match")
	}

	tlsA := ppscache.GetOrCreateTLS("namespace", "name")
	tlsB := ppscache.GetOrCreateTLS("namespace", "name")
	if tlsA != tlsB {
		log.Fatal("TLS dedup doesn't work")
	}
	if tlsA != ppscache.GetTls("namespace", "name") {
		log.Fatal("Got wrong TLS")
	}

	ppscache.GetTls("namespace", "name").Certificate = []byte{'a'}
	ppscache.GetTls("namespace", "name").Key = []byte{'b'}

	if ppscache.GetTls("namespace", "name").Certificate[0] != 'a' {
		log.Fatal("Error setting TLS")
	}

	ppscache.UpdateTLS("namespace", "name", []byte{'c'}, []byte{'d'})
	if ppscache.GetTls("namespace", "name").Certificate[0] != 'c' {
		log.Fatal("Error setting TLS")
	}
}
