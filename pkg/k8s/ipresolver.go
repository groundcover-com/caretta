package k8s

import (
	"context"
	"errors"
	"log"
	"net"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// gets a hostname (probably in the pattern name:namespace) and split it to name and namespace
// basically a wrapped Split function to handle some edge cases
func SplitNamespace(fullname string) (string, string) {
	if !strings.Contains(fullname, ":") {
		return fullname, ""
	}
	s := strings.Split(fullname, ":")
	if len(s) > 1 {
		return s[0], s[1]
	}
	return fullname, ""
}

type clusterSnapshot struct {
	Pods         map[types.UID]v1.Pod
	Nodes        map[types.UID]v1.Node
	ReplicaSets  map[types.UID]appsv1.ReplicaSet
	DaemonSets   map[types.UID]appsv1.DaemonSet
	StatefulSets map[types.UID]appsv1.StatefulSet
	Jobs         map[types.UID]batchv1.Job
	Services     map[types.UID]v1.Service
	Deployments  map[types.UID]appsv1.Deployment
	CronJobs     map[types.UID]batchv1.CronJob
}

type ipMapping map[string]string

type K8sIPResolver struct {
	clientset *kubernetes.Clientset
	snapshot  clusterSnapshot
	ipsMap    ipMapping
}

func NewIPResolver(clientset *kubernetes.Clientset) *K8sIPResolver {
	return &K8sIPResolver{
		clientset: clientset,
		snapshot: clusterSnapshot{
			Pods:         make(map[types.UID]v1.Pod),
			Nodes:        make(map[types.UID]v1.Node),
			ReplicaSets:  make(map[types.UID]appsv1.ReplicaSet),
			DaemonSets:   make(map[types.UID]appsv1.DaemonSet),
			StatefulSets: make(map[types.UID]appsv1.StatefulSet),
			Jobs:         make(map[types.UID]batchv1.Job),
			Services:     make(map[types.UID]v1.Service),
			Deployments:  make(map[types.UID]appsv1.Deployment),
			CronJobs:     make(map[types.UID]batchv1.CronJob),
		},
		ipsMap: make(ipMapping),
	}
}

// update the resolver's cache to the current cluster's state
func (resolver *K8sIPResolver) Update() error {
	if err := resolver.updateClusterSnapshot(); err != nil {
		return err
	}
	resolver.updateIpMapping()
	return nil
}

// resolve the given IP from the resolver's cache
// if not available, return the IP itself.
func (resolver *K8sIPResolver) ResolveIP(ip string) string {
	if val, ok := resolver.ipsMap[ip]; ok {
		return val
	}
	hosts, err := net.LookupAddr(ip)
	if err == nil && len(hosts) > 0 && hosts[0] != "" {
		result := hosts[0] + ":EXTERNAL"
		resolver.ipsMap[ip] = result
		return result
	}
	return ip
}

func (resolver *K8sIPResolver) updateClusterSnapshot() error {
	pods, err := resolver.clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting pods, aborting snapshot update")
	}
	resolver.snapshot.Pods = make(map[types.UID]v1.Pod)
	for _, pod := range pods.Items {
		resolver.snapshot.Pods[pod.UID] = pod
	}

	resolver.snapshot.Nodes = make(map[types.UID]v1.Node)
	nodes, err := resolver.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting nodes, aborting snapshot update")
	}
	for _, node := range nodes.Items {
		resolver.snapshot.Nodes[node.UID] = node
	}

	replicasets, err := resolver.clientset.AppsV1().ReplicaSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting replicasets, aborting snapshot update")
	}
	for _, rs := range replicasets.Items {
		resolver.snapshot.ReplicaSets[rs.ObjectMeta.UID] = rs
	}

	daemonsets, err := resolver.clientset.AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting daemonsets, aborting snapshot update")
	}
	for _, ds := range daemonsets.Items {
		resolver.snapshot.DaemonSets[ds.ObjectMeta.UID] = ds
	}

	statefulsets, err := resolver.clientset.AppsV1().StatefulSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting statefulsets, aborting snapshot update")
	}
	for _, ss := range statefulsets.Items {
		resolver.snapshot.StatefulSets[ss.ObjectMeta.UID] = ss
	}

	jobs, err := resolver.clientset.BatchV1().Jobs("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting jobs, aborting snapshot update")
	}
	for _, job := range jobs.Items {
		resolver.snapshot.Jobs[job.ObjectMeta.UID] = job
	}

	services, err := resolver.clientset.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting services, aborting snapshot update")
	}
	for _, service := range services.Items {
		resolver.snapshot.Services[service.UID] = service
	}

	deployments, err := resolver.clientset.AppsV1().Deployments("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting deployments, aborting snapshot update")
	}
	for _, deployment := range deployments.Items {
		resolver.snapshot.Deployments[deployment.UID] = deployment
	}

	cronJobs, err := resolver.clientset.BatchV1().CronJobs("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.New("error getting cronjobs, aborting snapshot update")
	}
	for _, cronJob := range cronJobs.Items {
		resolver.snapshot.CronJobs[cronJob.UID] = cronJob
	}

	return nil
}

// add mapping from ip to resolved host to an existing map,
// based on the given cluster snapshot
func (resolver *K8sIPResolver) updateIpMapping() {
	// to avoid long-term errors, we don't save hits for long
	resolver.ipsMap = make(ipMapping)

	// because IP collisions may occur and lead to overwritings in the map, the order is important
	// we go from less "favorable" to more "favorable" -
	// services -> running pods -> nodes

	for _, service := range resolver.snapshot.Services {
		// services has (potentially multiple) ClusterIP
		name := service.Name + ":" + service.Namespace

		// TODO maybe try to match service to workload
		for _, clusterIp := range service.Spec.ClusterIPs {
			if clusterIp != "None" {
				resolver.ipsMap[clusterIp] = name
			}
		}
	}

	for _, pod := range resolver.snapshot.Pods {
		name := resolver.resolvePodName(&pod)
		podPhase := pod.Status.Phase
		for _, podIp := range pod.Status.PodIPs {
			// if ip is already in the map, override only if current pod is running
			_, ok := resolver.ipsMap[podIp.IP]
			if !ok || podPhase == v1.PodRunning {
				resolver.ipsMap[podIp.IP] = name
			}
		}
	}

	for _, node := range resolver.snapshot.Nodes {
		for _, nodeAddress := range node.Status.Addresses {
			resolver.ipsMap[nodeAddress.Address] = string(nodeAddress.Type) + "/" + node.Name + ":INTERNAL"
		}
	}

	// localhost
	resolver.ipsMap["0.0.0.0"] = "localhost"
}

// an ugly function to go up one level in hierarchy. maybe there's a better way to do it
// the snapshot is maintained to avoid using an API request for each resolving
func (resolver *K8sIPResolver) getControllerOfOwner(snapshot *clusterSnapshot, originalOwner *metav1.OwnerReference) (*metav1.OwnerReference, error) {
	switch originalOwner.Kind {
	case "ReplicaSet":
		replicaSet, ok := snapshot.ReplicaSets[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing replicaset for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&replicaSet), nil
	case "DaemonSet":
		daemonset, ok := snapshot.DaemonSets[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing daemonset for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&daemonset), nil
	case "StatefulSet":
		statefulSet, ok := snapshot.StatefulSets[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing statefulset for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&statefulSet), nil
	case "Job":
		job, ok := snapshot.Jobs[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing job for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&job), nil
	case "Deployment":
		deployment, ok := snapshot.Deployments[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing deployment for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&deployment), nil
	case "CronJob":
		cronJob, ok := snapshot.CronJobs[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing cronjob for UID " + string(originalOwner.UID))
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
