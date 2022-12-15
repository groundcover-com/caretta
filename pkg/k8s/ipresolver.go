package k8s

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"

	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type clusterSnapshot struct {
	Pods         sync.Map // map[types.UID]v1.Pod
	Nodes        sync.Map // map[types.UID]v1.Node
	ReplicaSets  sync.Map // map[types.UID]appsv1.ReplicaSet
	DaemonSets   sync.Map // map[types.UID]appsv1.DaemonSet
	StatefulSets sync.Map // map[types.UID]appsv1.StatefulSet
	Jobs         sync.Map // map[types.UID]batchv1.Job
	Services     sync.Map // map[types.UID]v1.Service
	Deployments  sync.Map // map[types.UID]appsv1.Deployment
	CronJobs     sync.Map // map[types.UID]batchv1.CronJob
}

type ipMapping map[string]string

type K8sIPResolver struct {
	clientset  *kubernetes.Clientset
	snapshot   clusterSnapshot
	ipsMap     sync.Map
	stopSignal chan bool
}

func NewK8sIPResolver(clientset *kubernetes.Clientset) *K8sIPResolver {
	return &K8sIPResolver{
		clientset:  clientset,
		snapshot:   clusterSnapshot{},
		ipsMap:     sync.Map{},
		stopSignal: make(chan bool),
	}
}

// resolve the given IP from the resolver's cache
// if not available, return the IP itself.
func (resolver *K8sIPResolver) ResolveIP(ip string) string {
	if val, ok := resolver.ipsMap.Load(ip); ok {
		valString, ok := val.(string)
		if ok {
			return valString
		}
		log.Printf("type confusion in ipsMap")
	}
	hosts, err := net.LookupAddr(ip)
	if err == nil && len(hosts) > 0 && hosts[0] != "" {
		result := hosts[0] + ":EXTERNAL"
		resolver.ipsMap.Store(ip, result)
		return result
	}
	return ip
}

func (resolver *K8sIPResolver) StartWatching() error {
	// get initial state
	resolver.syncUpdateClusterSnapshot()
	resolver.updateIpMapping()

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
		select {
		case <-resolver.stopSignal:
			return
		case podEvent := <-podsWatcher.ResultChan():
			switch podEvent.Type {
			case watch.Added:
				pod, ok := podEvent.Object.(*v1.Pod)
				if !ok {
					break
				}
				resolver.snapshot.Pods.Store(pod.UID, pod)
				name := resolver.resolvePodName(pod)
				for _, podIp := range pod.Status.PodIPs {
					resolver.ipsMap.Store(podIp.IP, name)
				}
			case watch.Deleted:
				if val, ok := podEvent.Object.(*v1.Pod); ok {
					resolver.snapshot.Pods.Delete(val)
				}
			}
		case nodeEvent := <-nodesWatcher.ResultChan():
			switch nodeEvent.Type {
			case watch.Added:
				node, ok := nodeEvent.Object.(*v1.Node)
				if !ok {
					break
				}
				resolver.snapshot.Nodes.Store(node.UID, node)
				for _, nodeAddress := range node.Status.Addresses {
					resolver.ipsMap.Store(nodeAddress.Address, string(nodeAddress.Type)+"/"+node.Name+":INTERNAL")
				}
			case watch.Deleted:
				if val, ok := nodeEvent.Object.(*v1.Node); ok {
					resolver.snapshot.Nodes.Delete(val)
				}
			}
		case replicasetsEvent := <-replicasetsWatcher.ResultChan():
			switch replicasetsEvent.Type {
			case watch.Added:
				if val, ok := replicasetsEvent.Object.(*appsv1.ReplicaSet); ok {
					resolver.snapshot.ReplicaSets.Store(val.UID, val)
				}
			case watch.Deleted:
				if val, ok := replicasetsEvent.Object.(*appsv1.ReplicaSet); ok {
					resolver.snapshot.ReplicaSets.Delete(val)
				}
			}
		case daemonsetsEvent := <-daemonsetsWatcher.ResultChan():
			switch daemonsetsEvent.Type {
			case watch.Added:
				if val, ok := daemonsetsEvent.Object.(*appsv1.DaemonSet); ok {
					resolver.snapshot.DaemonSets.Store(val.UID, val)
				}
			case watch.Deleted:
				if val, ok := daemonsetsEvent.Object.(*appsv1.DaemonSet); ok {
					resolver.snapshot.DaemonSets.Delete(val)
				}
			}

		case statefulsetsEvent := <-statefulsetsWatcher.ResultChan():
			switch statefulsetsEvent.Type {
			case watch.Added:
				if val, ok := statefulsetsEvent.Object.(*appsv1.StatefulSet); ok {
					resolver.snapshot.StatefulSets.Store(val.UID, val)
				}
			case watch.Deleted:
				if val, ok := statefulsetsEvent.Object.(*appsv1.StatefulSet); ok {
					resolver.snapshot.StatefulSets.Delete(val)
				}
			}

		case jobsEvent := <-jobsWatcher.ResultChan():
			switch jobsEvent.Type {
			case watch.Added:
				if val, ok := jobsEvent.Object.(*batchv1.Job); ok {
					resolver.snapshot.Jobs.Store(val.UID, val)
				}
			case watch.Deleted:
				if val, ok := jobsEvent.Object.(*batchv1.Job); ok {
					resolver.snapshot.Jobs.Delete(val)
				}
			}

		case servicesEvent := <-servicesWatcher.ResultChan():
			switch servicesEvent.Type {
			case watch.Added:
				service, ok := servicesEvent.Object.(*v1.Service)
				if !ok {
					break
				}
				resolver.snapshot.Services.Store(service.UID, service)

				// services has (potentially multiple) ClusterIP
				name := service.Name + ":" + service.Namespace

				// TODO maybe try to match service to workload
				for _, clusterIp := range service.Spec.ClusterIPs {
					if clusterIp != "None" {
						resolver.ipsMap.Store(clusterIp, name)
					}
				}
			case watch.Deleted:
				if val, ok := servicesEvent.Object.(*v1.Service); ok {
					resolver.snapshot.Services.Delete(val)
				}
			}
		case deploymentsEvent := <-deploymentsWatcher.ResultChan():
			switch deploymentsEvent.Type {
			case watch.Added:
				if val, ok := deploymentsEvent.Object.(*appsv1.Deployment); ok {
					resolver.snapshot.Deployments.Store(val.UID, val)
				}
			case watch.Deleted:
				if val, ok := deploymentsEvent.Object.(*appsv1.Deployment); ok {
					resolver.snapshot.Deployments.Delete(val)
				}
			}

		case cronjobsEvent := <-cronJobsWatcher.ResultChan():
			switch cronjobsEvent.Type {
			case watch.Added:
				if val, ok := cronjobsEvent.Object.(*batchv1.CronJob); ok {
					resolver.snapshot.CronJobs.Store(val.UID, val)
				}
			case watch.Deleted:
				if val, ok := cronjobsEvent.Object.(*batchv1.CronJob); ok {
					resolver.snapshot.CronJobs.Delete(val)
				}
			}
		}
	}()

	return nil
}

func (resolver *K8sIPResolver) StopWatching() {
	resolver.stopSignal <- true
}

// iterate the API for initial coverage of the cluster's state
func (resolver *K8sIPResolver) syncUpdateClusterSnapshot() error {
	pods, err := resolver.clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting pods, aborting snapshot update")
	}
	resolver.snapshot.Pods = sync.Map{}
	for _, pod := range pods.Items {
		resolver.snapshot.Pods.Store(pod.UID, pod)
	}

	resolver.snapshot.Nodes = sync.Map{}
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
		name := service.Name + ":" + service.Namespace

		// TODO maybe try to match service to workload
		for _, clusterIp := range service.Spec.ClusterIPs {
			if clusterIp != "None" {
				resolver.ipsMap.Store(clusterIp, name)
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
		name := resolver.resolvePodName(&pod)
		podPhase := pod.Status.Phase
		for _, podIp := range pod.Status.PodIPs {
			// if ip is already in the map, override only if current pod is running
			_, ok := resolver.ipsMap.Load(podIp.IP)
			if !ok || podPhase == v1.PodRunning {
				resolver.ipsMap.Store(podIp.IP, name)
			}
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
			resolver.ipsMap.Store(nodeAddress.Address, string(nodeAddress.Type)+"/"+node.Name+":INTERNAL")
		}
		return true
	})

	// localhost
	resolver.ipsMap.Store("0.0.0.0", "localhost")
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

func (resolver *K8sIPResolver) resolvePodName(pod *v1.Pod) string {
	name := pod.Name + ":" + pod.Namespace
	owner := metav1.GetControllerOf(pod)
	for owner != nil {
		var err error
		name = owner.Name + ":" + pod.Namespace
		owner, err = resolver.getControllerOfOwner(&resolver.snapshot, owner)
		if err != nil {
			log.Printf("Error retreiving owner of %v - %v", name, err)
		}
	}
	return name
}
