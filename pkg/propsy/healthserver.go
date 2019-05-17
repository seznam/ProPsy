package propsy

import (
	"net/http"
)

type HealthServer struct {
	listen string
	isReady bool
	isHealthy bool
}

func NewHealthServer(listen string) *HealthServer {
	return &HealthServer{listen: listen}
}

func (H *HealthServer) SetReady(ready bool) {
	H.isReady = ready
}

func (H *HealthServer) SetHealthy(healthy bool) {
	H.isHealthy = healthy
}

func (H *HealthServer) Start() {
	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if H.isReady {
			H.sendOK(w, r)
		} else {
			H.sendBad(w, r)
		}
	})

	http.HandleFunc("/healthy", func(w http.ResponseWriter, r *http.Request) {
		if H.isHealthy {
			H.sendOK(w, r)
		} else {
			H.sendBad(w, r)
		}
	})

	go func() {
		http.ListenAndServe(H.listen, nil)
	}()
}

func (H *HealthServer) sendOK(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "ok", http.StatusOK)
}

func (H *HealthServer) sendBad(w http.ResponseWriter, r *http.Request) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}