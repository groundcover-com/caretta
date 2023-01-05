package k8s

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	lrucache "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const MAX_RESOLVED_DNS = 10000 // arbitrary limit
var reregisterWatchSleepDuration = 1 * time.Second

var (
	watchEventsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "caretta_watcher_events_count",
	}, []string{"object_type"})
	watchResetsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "caretta_watcher_resets_count",
	}, []string{"object_type"})
)

type clusterSnapshot struct {
	Pods           sync.Map // map[types.UID]v1.Pod
	Nodes          sync.Map // map[types.UID]v1.Node
	ReplicaSets    sync.Map // map[types.UID]appsv1.ReplicaSet
	DaemonSets     sync.Map // map[types.UID]appsv1.DaemonSet
	StatefulSets   sync.Map // map[types.UID]appsv1.StatefulSet
	Jobs           sync.Map // map[types.UID]batchv1.Job
	Services       sync.Map // map[types.UID]v1.Service
	Deployments    sync.Map // map[types.UID]appsv1.Deployment
	CronJobs       sync.Map // map[types.UID]batchv1.CronJob
	PodDescriptors sync.Map // map[types.UID]string
}

type K8sIPResolver struct {
	clientset        kubernetes.Interface
	snapshot         clusterSnapshot
	ipsMap           sync.Map
	stopSignal       chan bool
	shouldResolveDns bool
	dnsResolvedIps   *lrucache.Cache[string, string]
}

type Workload struct {
	Name      string
	Namespace string
	Kind      string
}

func NewK8sIPResolver(clientset kubernetes.Interface, resolveDns bool) (*K8sIPResolver, error) {
	var dnsCache *lrucache.Cache[string, string]
	if resolveDns {
		var err error
		dnsCache, err = lrucache.New[string, string](MAX_RESOLVED_DNS)
		if err != nil {
			return nil, err
		}
	} else {
		dnsCache = nil
	}
	return &K8sIPResolver{
		clientset:        clientset,
		snapshot:         clusterSnapshot{},
		ipsMap:           sync.Map{},
		stopSignal:       make(chan bool),
		shouldResolveDns: resolveDns,
		dnsResolvedIps:   dnsCache,
	}, nil
}

// resolve the given IP from the resolver's cache
// if not available, return the IP itself.
func (resolver *K8sIPResolver) ResolveIP(ip string) Workload {
	if val, ok := resolver.ipsMap.Load(ip); ok {
		entry, ok := val.(Workload)
		if ok {
			return entry
		}
		log.Printf("type confusion in ipsMap")
	}
	host := ip

	if resolver.shouldResolveDns {
		val, ok := resolver.dnsResolvedIps.Get(ip)
		if ok {
			host = val
		} else {
			hosts, err := net.LookupAddr(ip)
			if err == nil && len(hosts) > 0 {
				host = hosts[0]
			}
			resolver.dnsResolvedIps.Add(ip, host)
		}
	}
	return Workload{
		Name:      host,
		Namespace: "external",
		Kind:      "external",
	}
}

func (resolver *K8sIPResolver) StartWatching() error {
	// register watchers
	podsWatcher, err := resolver.clientset.CoreV1().Pods("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching pods changes")
	}

	nodesWatcher, err := resolver.clientset.CoreV1().Nodes().Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching nodes changes")
	}

	replicasetsWatcher, err := resolver.clientset.AppsV1().ReplicaSets("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching replicasets changes")
	}

	daemonsetsWatcher, err := resolver.clientset.AppsV1().DaemonSets("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching daemonsets changes")
	}

	statefulsetsWatcher, err := resolver.clientset.AppsV1().StatefulSets("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching statefulsets changes")
	}

	jobsWatcher, err := resolver.clientset.BatchV1().Jobs("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching jobs changes")
	}

	servicesWatcher, err := resolver.clientset.CoreV1().Services("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching services changes")
	}

	deploymentsWatcher, err := resolver.clientset.AppsV1().Deployments("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watcher deployments changes")
	}

	cronJobsWatcher, err := resolver.clientset.BatchV1().CronJobs("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error watching cronjobs changes")
	}

	// invoke a watching function
	go func() {
		for {
			select {
			case <-resolver.stopSignal:
				podsWatcher.Stop()
				nodesWatcher.Stop()
				replicasetsWatcher.Stop()
				daemonsetsWatcher.Stop()
				statefulsetsWatcher.Stop()
				jobsWatcher.Stop()
				servicesWatcher.Stop()
				deploymentsWatcher.Stop()
				cronJobsWatcher.Stop()
				return
			case podEvent, ok := <-podsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("pod").Inc()
						podsWatcher, err = resolver.clientset.CoreV1().Pods("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("pod").Inc()
					resolver.handlePodWatchEvent(&podEvent)
				}
			case nodeEvent, ok := <-nodesWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("node").Inc()
						nodesWatcher, err = resolver.clientset.CoreV1().Nodes().Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("node").Inc()
					resolver.handleNodeWatchEvent(&nodeEvent)
				}
			case replicasetsEvent, ok := <-replicasetsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("replicaset").Inc()
						replicasetsWatcher, err = resolver.clientset.AppsV1().ReplicaSets("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("replicaset").Inc()
					resolver.handleReplicaSetWatchEvent(&replicasetsEvent)
				}
			case daemonsetsEvent, ok := <-daemonsetsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("daemonset").Inc()
						daemonsetsWatcher, err = resolver.clientset.AppsV1().DaemonSets("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("daemonset").Inc()
					resolver.handleDaemonSetWatchEvent(&daemonsetsEvent)
				}
			case statefulsetsEvent, ok := <-statefulsetsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("statefulset").Inc()
						statefulsetsWatcher, err = resolver.clientset.AppsV1().StatefulSets("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("statefulset").Inc()
					resolver.handleStatefulSetWatchEvent(&statefulsetsEvent)
				}
			case jobsEvent, ok := <-jobsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("job").Inc()
						jobsWatcher, err = resolver.clientset.BatchV1().Jobs("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("job").Inc()
					resolver.handleJobsWatchEvent(&jobsEvent)
				}
			case servicesEvent, ok := <-servicesWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("service").Inc()
						servicesWatcher, err = resolver.clientset.CoreV1().Services("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("service").Inc()
					resolver.handleServicesWatchEvent(&servicesEvent)
				}
			case deploymentsEvent, ok := <-deploymentsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("deployment").Inc()
						deploymentsWatcher, err = resolver.clientset.AppsV1().Deployments("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("deployment").Inc()
					resolver.handleDeploymentsWatchEvent(&deploymentsEvent)
				}
			case cronjobsEvent, ok := <-cronJobsWatcher.ResultChan():
				{
					if !ok {
						watchResetsCounter.WithLabelValues("cronjob").Inc()
						cronJobsWatcher, err = resolver.clientset.BatchV1().CronJobs("").Watch(context.Background(), metav1.ListOptions{})
						if err != nil {
							time.Sleep(reregisterWatchSleepDuration)
						}
						continue
					}
					watchEventsCounter.WithLabelValues("cronjob").Inc()
					resolver.handleCronJobsWatchEvent(&cronjobsEvent)
				}
			}
		}
	}()

	// get initial state
	err = resolver.getResolvedClusterSnapshot()
	if err != nil {
		resolver.StopWatching()
		return fmt.Errorf("error retrieving cluster's initial state: %v", err)
	}

	return nil
}

func (resolver *K8sIPResolver) StopWatching() {
	resolver.stopSignal <- true
}

func (resolver *K8sIPResolver) handlePodWatchEvent(podEvent *watch.Event) {
	switch podEvent.Type {
	case watch.Added:
		pod, ok := podEvent.Object.(*v1.Pod)
		if !ok {
			return
		}
		resolver.snapshot.Pods.Store(pod.UID, *pod)
		entry := resolver.resolvePodDescriptor(pod)
		for _, podIp := range pod.Status.PodIPs {
			resolver.storeWorkloadsIP(podIp.IP, &entry)
		}
	case watch.Modified:
		pod, ok := podEvent.Object.(*v1.Pod)
		if !ok {
			return
		}
		resolver.snapshot.Pods.Store(pod.UID, *pod)
		entry := resolver.resolvePodDescriptor(pod)
		for _, podIp := range pod.Status.PodIPs {
			resolver.storeWorkloadsIP(podIp.IP, &entry)
		}
	case watch.Deleted:
		if val, ok := podEvent.Object.(*v1.Pod); ok {
			resolver.snapshot.Pods.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleNodeWatchEvent(nodeEvent *watch.Event) {
	switch nodeEvent.Type {
	case watch.Added, watch.Modified:
		node, ok := nodeEvent.Object.(*v1.Node)
		if !ok {
			return
		}
		resolver.snapshot.Nodes.Store(node.UID, *node)
		for _, nodeAddress := range node.Status.Addresses {
			resolver.storeWorkloadsIP(nodeAddress.Address, &Workload{
				Name:      node.Name,
				Namespace: "node",
				Kind:      "node",
			})
		}
	case watch.Deleted:
		if val, ok := nodeEvent.Object.(*v1.Node); ok {
			resolver.snapshot.Nodes.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleReplicaSetWatchEvent(replicasetsEvent *watch.Event) {
	switch replicasetsEvent.Type {
	case watch.Added:
		if val, ok := replicasetsEvent.Object.(*appsv1.ReplicaSet); ok {
			resolver.snapshot.ReplicaSets.Store(val.UID, *val)
		}
	case watch.Deleted:
		if val, ok := replicasetsEvent.Object.(*appsv1.ReplicaSet); ok {
			resolver.snapshot.ReplicaSets.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleDaemonSetWatchEvent(daemonsetsEvent *watch.Event) {
	switch daemonsetsEvent.Type {
	case watch.Added:
		if val, ok := daemonsetsEvent.Object.(*appsv1.DaemonSet); ok {
			resolver.snapshot.DaemonSets.Store(val.UID, *val)
		}
	case watch.Deleted:
		if val, ok := daemonsetsEvent.Object.(*appsv1.DaemonSet); ok {
			resolver.snapshot.DaemonSets.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleStatefulSetWatchEvent(statefulsetsEvent *watch.Event) {
	switch statefulsetsEvent.Type {
	case watch.Added:
		if val, ok := statefulsetsEvent.Object.(*appsv1.StatefulSet); ok {
			resolver.snapshot.StatefulSets.Store(val.UID, *val)
		}
	case watch.Deleted:
		if val, ok := statefulsetsEvent.Object.(*appsv1.StatefulSet); ok {
			resolver.snapshot.StatefulSets.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleJobsWatchEvent(jobsEvent *watch.Event) {
	switch jobsEvent.Type {
	case watch.Added:
		if val, ok := jobsEvent.Object.(*batchv1.Job); ok {
			resolver.snapshot.Jobs.Store(val.UID, *val)
		}
	case watch.Deleted:
		if val, ok := jobsEvent.Object.(*batchv1.Job); ok {
			resolver.snapshot.Jobs.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleServicesWatchEvent(servicesEvent *watch.Event) {
	switch servicesEvent.Type {
	case watch.Added, watch.Modified:
		service, ok := servicesEvent.Object.(*v1.Service)
		if !ok {
			return
		}
		resolver.snapshot.Services.Store(service.UID, *service)

		// services has (potentially multiple) ClusterIP
		workload := Workload{
			Name:      service.Name,
			Namespace: service.Namespace,
			Kind:      "Service",
		}

		// TODO maybe try to match service to workload
		for _, clusterIp := range service.Spec.ClusterIPs {
			if clusterIp != "None" {
				_, ok := resolver.ipsMap.Load(clusterIp)
				if !ok {
					resolver.storeWorkloadsIP(clusterIp, &workload)
				}
			}
		}
	case watch.Deleted:
		if val, ok := servicesEvent.Object.(*v1.Service); ok {
			resolver.snapshot.Services.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleDeploymentsWatchEvent(deploymentsEvent *watch.Event) {
	switch deploymentsEvent.Type {
	case watch.Added:
		if val, ok := deploymentsEvent.Object.(*appsv1.Deployment); ok {
			resolver.snapshot.Deployments.Store(val.UID, *val)
		}
	case watch.Deleted:
		if val, ok := deploymentsEvent.Object.(*appsv1.Deployment); ok {
			resolver.snapshot.Deployments.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) handleCronJobsWatchEvent(cronjobsEvent *watch.Event) {
	switch cronjobsEvent.Type {
	case watch.Added:
		if val, ok := cronjobsEvent.Object.(*batchv1.CronJob); ok {
			resolver.snapshot.CronJobs.Store(val.UID, *val)
		}
	case watch.Deleted:
		if val, ok := cronjobsEvent.Object.(*batchv1.CronJob); ok {
			resolver.snapshot.CronJobs.Delete(val.UID)
		}
	}
}

func (resolver *K8sIPResolver) getResolvedClusterSnapshot() error {
	err := resolver.getFullClusterSnapshot()
	if err != nil {
		return err
	}
	resolver.updateIpMapping()
	return nil
}

// iterate the API for initial coverage of the cluster's state
func (resolver *K8sIPResolver) getFullClusterSnapshot() error {
	pods, err := resolver.clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting pods, aborting snapshot update")
	}
	for _, pod := range pods.Items {
		resolver.snapshot.Pods.Store(pod.UID, pod)
	}

	nodes, err := resolver.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting nodes, aborting snapshot update")
	}
	for _, node := range nodes.Items {
		resolver.snapshot.Nodes.Store(node.UID, node)
	}

	replicasets, err := resolver.clientset.AppsV1().ReplicaSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting replicasets, aborting snapshot update")
	}
	for _, rs := range replicasets.Items {
		resolver.snapshot.ReplicaSets.Store(rs.ObjectMeta.UID, rs)
	}

	daemonsets, err := resolver.clientset.AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting daemonsets, aborting snapshot update")
	}
	for _, ds := range daemonsets.Items {
		resolver.snapshot.DaemonSets.Store(ds.ObjectMeta.UID, ds)
	}

	statefulsets, err := resolver.clientset.AppsV1().StatefulSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting statefulsets, aborting snapshot update")
	}
	for _, ss := range statefulsets.Items {
		resolver.snapshot.StatefulSets.Store(ss.ObjectMeta.UID, ss)
	}

	jobs, err := resolver.clientset.BatchV1().Jobs("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting jobs, aborting snapshot update")
	}
	for _, job := range jobs.Items {
		resolver.snapshot.Jobs.Store(job.ObjectMeta.UID, job)
	}

	services, err := resolver.clientset.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting services, aborting snapshot update")
	}
	for _, service := range services.Items {
		resolver.snapshot.Services.Store(service.UID, service)
	}

	deployments, err := resolver.clientset.AppsV1().Deployments("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting deployments, aborting snapshot update")
	}
	for _, deployment := range deployments.Items {
		resolver.snapshot.Deployments.Store(deployment.UID, deployment)
	}

	cronJobs, err := resolver.clientset.BatchV1().CronJobs("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting cronjobs, aborting snapshot update")
	}
	for _, cronJob := range cronJobs.Items {
		resolver.snapshot.CronJobs.Store(cronJob.UID, cronJob)
	}

	return nil
}

// add mapping from ip to resolved host to an existing map,
// based on the given cluster snapshot
func (resolver *K8sIPResolver) updateIpMapping() {
	// because IP collisions may occur and lead to overwritings in the map, the order is important
	// we go from less "favorable" to more "favorable" -
	// services -> running pods -> nodes

	resolver.snapshot.Services.Range(func(key any, val any) bool {
		service, ok := val.(v1.Service)
		if !ok {
			log.Printf("Type confusion in services map")
			return true // continue
		}
		// services has (potentially multiple) ClusterIP
		workload := Workload{
			Name:      service.Name,
			Namespace: service.Namespace,
			Kind:      "Service",
		}

		// TODO maybe try to match service to workload
		for _, clusterIp := range service.Spec.ClusterIPs {
			if clusterIp != "None" {
				resolver.storeWorkloadsIP(clusterIp, &workload)
			}
		}
		return true
	})

	resolver.snapshot.Pods.Range(func(key, value any) bool {
		pod, ok := value.(v1.Pod)
		if !ok {
			log.Printf("Type confusion in pods map")
			return true // continue
		}
		entry := resolver.resolvePodDescriptor(&pod)
		for _, podIp := range pod.Status.PodIPs {
			// if ip is already in the map, override only if current pod is running
			resolver.storeWorkloadsIP(podIp.IP, &entry)
		}
		return true
	})

	resolver.snapshot.Nodes.Range(func(key any, value any) bool {
		node, ok := value.(v1.Node)
		if !ok {
			log.Printf("Type confusion in nodes map")
			return true // continue
		}
		for _, nodeAddress := range node.Status.Addresses {
			workload := Workload{
				Name:      node.Name,
				Namespace: "node",
				Kind:      "node",
			}
			resolver.storeWorkloadsIP(nodeAddress.Address, &workload)
		}
		return true
	})
}

func (resolver *K8sIPResolver) storeWorkloadsIP(ip string, newWorkload *Workload) {
	// we want to override existing workload, unless the existing workload is a node and the new one isn't
	val, ok := resolver.ipsMap.Load(ip)
	if ok {
		existingWorkload, ok := val.(Workload)
		if ok {
			if existingWorkload.Kind == "node" && newWorkload.Kind != "node" {
				return
			}
		}
	}
	resolver.ipsMap.Store(ip, *newWorkload)
}

// an ugly function to go up one level in hierarchy. maybe there's a better way to do it
// the snapshot is maintained to avoid using an API request for each resolving
func (resolver *K8sIPResolver) getControllerOfOwner(snapshot *clusterSnapshot, originalOwner *metav1.OwnerReference) (*metav1.OwnerReference, error) {
	switch originalOwner.Kind {
	case "ReplicaSet":
		replicaSetVal, ok := snapshot.ReplicaSets.Load(originalOwner.UID)
		if !ok {
			return nil, errors.New("Missing replicaset for UID " + string(originalOwner.UID))
		}
		replicaSet, ok := replicaSetVal.(appsv1.ReplicaSet)
		if !ok {
			return nil, errors.New("type confusion in replicasets map")
		}
		return metav1.GetControllerOf(&replicaSet), nil
	case "DaemonSet":
		daemonSetVal, ok := snapshot.DaemonSets.Load(originalOwner.UID)
		if !ok {
			return nil, errors.New("Missing daemonset for UID " + string(originalOwner.UID))
		}
		daemonSet, ok := daemonSetVal.(appsv1.DaemonSet)
		if !ok {
			return nil, errors.New("type confusion in daemonsets map")
		}
		return metav1.GetControllerOf(&daemonSet), nil
	case "StatefulSet":
		statefulSetVal, ok := snapshot.StatefulSets.Load(originalOwner.UID)
		if !ok {
			return nil, errors.New("Missing statefulset for UID " + string(originalOwner.UID))
		}
		statefulSet, ok := statefulSetVal.(appsv1.StatefulSet)
		if !ok {
			return nil, errors.New("type confusion in statefulsets map")
		}
		return metav1.GetControllerOf(&statefulSet), nil
	case "Job":
		jobVal, ok := snapshot.Jobs.Load(originalOwner.UID)
		if !ok {
			return nil, errors.New("Missing job for UID " + string(originalOwner.UID))
		}
		job, ok := jobVal.(batchv1.Job)
		if !ok {
			return nil, errors.New("type confusion in jobs map")
		}
		return metav1.GetControllerOf(&job), nil
	case "Deployment":
		deploymentVal, ok := snapshot.Deployments.Load(originalOwner.UID)
		if !ok {
			return nil, errors.New("Missing deployment for UID " + string(originalOwner.UID))
		}
		deployment, ok := deploymentVal.(appsv1.Deployment)
		if !ok {
			return nil, errors.New("type confusion in deployments map")
		}
		return metav1.GetControllerOf(&deployment), nil
	case "CronJob":
		cronJobVal, ok := snapshot.CronJobs.Load(originalOwner.UID)
		if !ok {
			return nil, errors.New("Missing cronjob for UID " + string(originalOwner.UID))
		}
		cronJob, ok := cronJobVal.(batchv1.CronJob)
		if !ok {
			return nil, errors.New("type confusion in cronjobs map")
		}
		return metav1.GetControllerOf(&cronJob), nil
	}
	return nil, errors.New("Unsupported kind for lookup - " + originalOwner.Kind)
}

func (resolver *K8sIPResolver) resolvePodDescriptor(pod *v1.Pod) Workload {
	existing, ok := resolver.snapshot.PodDescriptors.Load(pod.UID)
	if ok {
		result, ok := existing.(Workload)
		if ok {
			return result
		}
	}
	var err error
	name := pod.Name
	namespace := pod.Namespace
	kind := "pod"
	owner := metav1.GetControllerOf(pod)
	for owner != nil {
		name = owner.Name
		kind = owner.Kind
		owner, err = resolver.getControllerOfOwner(&resolver.snapshot, owner)
		if err != nil {
			log.Printf("Error retreiving owner of %v - %v", name, err)
		}
	}
	result := Workload{
		Name:      name,
		Namespace: namespace,
		Kind:      kind,
	}
	if err == nil {
		resolver.snapshot.PodDescriptors.Store(pod.UID, result)
	}
	return result
}
