package metrics

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func StartMetricsServer(endpoint string, port string) *http.Server {
	http.Handle(endpoint, promhttp.Handler())
	server := &http.Server{Addr: port}
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			log.Fatalf("Error starting prometheus server on port %v", port)
		}
	}()
	return server
}
