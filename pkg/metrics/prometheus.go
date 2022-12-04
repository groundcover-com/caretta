package metrics

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func StartMetricsServer(endpoint string, port string) {
	http.Handle(endpoint, promhttp.Handler())
	go func() {
		err := http.ListenAndServe(port, nil)
		if err != nil {
			log.Fatalf("Error starting prometheus server on port %v", port)
		}
	}()
}
