package caretta

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	caretta_k8s "github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/groundcover-com/caretta/pkg/metrics"
	"github.com/groundcover-com/caretta/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultPrometheusEndpoint     = "/metrics"
	defaultPrometheusPort         = ":7117"
	defaultPollingIntervalSeconds = 5
	defaultShouldResolveDns       = false
)

var (
	linksMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "caretta_links_observed",
		Help: "total bytes_sent value of links observed by caretta since its launch",
	}, []string{
		"LinkId", "ClientId", "ClientName", "ClientNamespace", "ServerId", "ServerName", "ServerNamespace", "ServerPort", "Role",
	})
)

type Caretta struct {
	stopSignal    chan bool
	tracer        tracing.LinksTracer
	metricsServer *http.Server
	config        carettaConfig
}

// hold configurable values
type carettaConfig struct {
	shouldResolveDns       bool
	prometheusPort         string
	prometheusEndpoint     string
	pollingIntervalSeconds int
}

func NewCaretta() *Caretta {
	return &Caretta{
		stopSignal: make(chan bool, 1),
		config:     readConfig(),
	}
}

func (caretta *Caretta) Start() {
	caretta.metricsServer = metrics.StartMetricsServer(defaultPrometheusEndpoint, defaultPrometheusPort)

	clientset, err := caretta.getClientSet()
	if err != nil {
		log.Fatalf("Error getting kubernetes clientset: %v", err)
	}
	resolver := caretta_k8s.NewK8sIPResolver(clientset, caretta.config.shouldResolveDns)
	if resolver.StartWatching() != nil {
		log.Fatalf("Error watching cluster's state: %v", err)
	}

	// wait for resolver to populate
	time.Sleep(10 * time.Second)

	caretta.tracer = tracing.NewTracer(resolver)
	err = caretta.tracer.Start()
	if err != nil {
		log.Fatalf("Couldn't load probes - %v", err)
	}

	pollingTicker := time.NewTicker(time.Duration(caretta.config.pollingIntervalSeconds) * time.Second)

	pastLinks := make(map[tracing.NetworkLink]uint64)

	go func() {
		for {
			select {
			case <-caretta.stopSignal:
				return
			case <-pollingTicker.C:
				var links map[tracing.NetworkLink]uint64

				if err != nil {
					log.Printf("Error updating snapshot of cluster state, skipping iteration")
					continue
				}

				pastLinks, links = caretta.tracer.TracesPollingIteration(pastLinks)
				for link, throughput := range links {
					caretta.handleLink(&link, throughput)
				}
			}
		}
	}()
}

func (caretta *Caretta) Stop() {
	log.Print("Stopping Caretta...")
	caretta.stopSignal <- true
	err := caretta.tracer.Stop()
	if err != nil {
		log.Printf("Error unloading bpf objects: %v", err)
	}
	err = caretta.metricsServer.Shutdown(context.Background())
	if err != nil {
		log.Printf("Error shutting Prometheus server down: %v", err)
	}

}

// environment variables based, encaplsulated to enable future changes
func readConfig() carettaConfig {
	port := defaultPrometheusPort
	if val := os.Getenv("PROM_PORT"); val != "" {
		valInt, err := strconv.Atoi(val)
		if err == nil {
			port = fmt.Sprintf(":%d", valInt)
		}
	}

	endpoint := defaultPrometheusEndpoint
	if val := os.Getenv("PROM_ENDPOINT"); val != "" {
		endpoint = val
	}

	interval := defaultPollingIntervalSeconds
	if val := os.Getenv("POLL_INTERVAL"); val != "" {
		valInt, err := strconv.Atoi(val)
		if err == nil {
			interval = valInt
		}
	}

	shouldResolveDns := defaultShouldResolveDns
	if val := os.Getenv("RESOLVE_DNS"); val != "" {
		valBool, err := strconv.ParseBool(val)
		if err == nil {
			shouldResolveDns = valBool
		}
	}

	return carettaConfig{
		shouldResolveDns:       shouldResolveDns,
		prometheusPort:         port,
		prometheusEndpoint:     endpoint,
		pollingIntervalSeconds: interval,
	}
}

func (caretta *Caretta) handleLink(link *tracing.NetworkLink, throughput uint64) {
	clientName, clientNamespace := splitNamespace(link.ClientHost)
	serverName, serverNamespace := splitNamespace(link.ServerHost)
	linksMetrics.With(prometheus.Labels{
		"LinkId":          strconv.Itoa(int(fnvHash(link.ClientHost+link.ServerHost) + link.Role)),
		"ClientId":        strconv.Itoa(int(fnvHash(link.ClientHost))),
		"ClientName":      clientName,
		"ClientNamespace": clientNamespace,
		"ServerId":        strconv.Itoa(int(fnvHash(link.ServerHost))),
		"ServerName":      serverName,
		"ServerNamespace": serverNamespace,
		"ServerPort":      strconv.Itoa(int(link.ServerPort)),
		"Role":            strconv.Itoa(int(link.Role)),
	}).Set(float64(throughput))
}

func (caretta *Caretta) getClientSet() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// simple fnvHash function from string to uint32
func fnvHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// gets a hostname (probably in the pattern name:namespace) and split it to name and namespace
// basically a wrapped Split function to handle some edge cases
func splitNamespace(fullname string) (string, string) {
	if !strings.Contains(fullname, ":") {
		return fullname, ""
	}
	s := strings.Split(fullname, ":")
	if len(s) > 1 {
		return s[0], s[1]
	}
	return fullname, ""
}
