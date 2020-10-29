package main


import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"

	"github.com/julienschmidt/httprouter"
	"github.com/gopswamy/rit-k8s-rdma-common/knapsack_pod_placement"
	"github.com/gopswamy/rit-k8s-rdma-common/rdma_hardware_info"

	"k8s.io/api/core/v1"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
)

const (
	RdmaSchedulerExtenderDefaultPort string = "8888"
	RdmaSchedulerExtenderHttpListenPath string = "/scheduler/rdma_scheduling"
	RdmaSchedulerNodeQueryTimeout int = 1500
)

//structure describing whether or not a pod can be scheduled on a specific
//	node. this type is passed through the channel from 'queryNode' to
//	'HandleSchedulerFilterRequest'
type node_eligibility struct {
	capacity int
	index int
	enough_resources bool
	ineligibility_reason string
}

//queryNode takes in the address(es) of a single potential node, a list of the
//	RDMA resources needed by a pod, and a channel to send the results back
//	in. it queries the RDMA hardware DaemonSet on the potential node, then
//	determines if that node's resources are enough to satisfy the pod's
//	request. the result is then passed back through the channel.
func queryNode(node_index int,
	node_addresses []v1.NodeAddress,
	needed_resources []knapsack_pod_placement.RdmaInterfaceRequest,
	output_channel chan<- node_eligibility) {

	//set up the result structure and fill initialize it with an id of the
	//	node we are processing
	var node_result node_eligibility
	node_result.index = node_index

	//iterate through the node's internal addresses (those reachable from
	//	within the k8s cluster) until we find one at which we can
	//	reach the node.
	for _, node_addr := range node_addresses {
		if((node_addr.Type == v1.NodeInternalIP) || (node_addr.Type == v1.NodeInternalDNS)) {
			//query the node for what RDMA resources it has available
			pfs, err := rdma_hardware_info.QueryNode(node_addr.Address, rdma_hardware_info.DefaultPort, RdmaSchedulerNodeQueryTimeout)
			//if an error occured while querying the node, try the next address
			if(err != nil) {
				continue
			}

			//determine if the node's avilable resources will satisfy the pod's needs
			capacity,_, placement_success := knapsack_pod_placement.PlacePod(needed_resources, pfs, false)

			//if the pod's needs couldn't be met
			if(!placement_success) {
				//report that back through the channel
				node_result.enough_resources = false
				node_result.ineligibility_reason = "RDMA Scheduler Extension: Node did not have enough free RDMA resources."
				node_result.capacity = capacity
				output_channel <- node_result
				return
			//otherwise, the pod's needs could be met
			} else {
				//report that back through the channel
				node_result.enough_resources = true
				node_result.ineligibility_reason = ""
				node_result.capacity = capacity
				output_channel <- node_result
				return
			}
		}
	}

	//if we couldn't reach the DaemonSet on the node, return a result
	//	stating that.
	node_result.enough_resources = false
	node_result.ineligibility_reason = "RDMA Scheduler Extension: Unable to collect information on available RDMA resources for node."
	output_channel <- node_result
	return
}


// HandleSchedulerFilterRequest is a callback function that processes incoming
//	HTTP requests to the RDMA scheduler extender.
func HandleSchedulerFilterRequest(response http.ResponseWriter, request *http.Request, _ httprouter.Params) {
	log.Print("\n")
	log.Print("\n")

	//reject empty requests
	if(request.Body == nil) {
		log.Println("Got empty http request.")
		http.Error(response, "Request body was empty.", 400)
		return
	}

	//we fill in the response structure with information about our
	//	scheduling decisions. the result will always be a JSON
	//	sctructure.
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)

	//scheduler extender input that can be parsed from the incoming request
	var sched_extender_args schedulerapi.ExtenderArgs
	//scheduler extender output that we will fill in
	var extender_filter_results *schedulerapi.ExtenderFilterResult

	//decode the scheduler extender input. this gives us both the list of
	//	potential nodes to schedule on, and the details of the pod to
	//	be scheduled.
	err := json.NewDecoder(request.Body).Decode(&sched_extender_args)
	if(err != nil) {
		log.Println("Got http request with malformatted scheduler extender arguments.")
		//if decoding failed, return an empty result
		extender_filter_results = &schedulerapi.ExtenderFilterResult{
			Nodes:       nil,
			FailedNodes: nil,
			Error:       err.Error(),
		}
	//otherwise, decoding the incoming request was successful
	} else {
		log.Println("Got request to schedule pod: ", sched_extender_args.Pod.ObjectMeta.Name)
		log.Println("Potential nodes to schedule on (and their addresses):")
		for _, node := range sched_extender_args.Nodes.Items {
			log.Print("\t", node.Name, ": ", node.Status.Addresses)
		}

		//fill in two structures, one for nodes that can support the
		//	pod, and another for nodes that cannot. the type of
		//	these structures is dictated by the k8s scheduler API.
       		canSchedule := make([]v1.Node, 0, len(sched_extender_args.Nodes.Items))
		canNotSchedule := make(map[string]string)

		//the channel over which results about whether each node can
		//	support the pod are passed.
		node_eligibility_channel := make(chan node_eligibility)

		//read the annotations from the pod's YAML file (this is where
		//	the information about requested RDMA resources is stored).
		pod_annotations := sched_extender_args.Pod.ObjectMeta.Annotations
		//if the pod does not require any RDMA interfaces
		if(pod_annotations["rdma_interfaces_required"] == "") {
			log.Println("Pod doesn't require any RDMA interfaces. No nodes will be filtered out.")
			//we don't filter out any of the potential nodes
			for _, node := range sched_extender_args.Nodes.Items {
				canSchedule = append(canSchedule, node)
			}
		//otherwise, if the pod does need one or more RDMA interfaces
		} else {
			//parse the JSON specifying the needed RDMA interfaces
			//	into the relevant structure.
			var interfaces_needed []knapsack_pod_placement.RdmaInterfaceRequest
			err = json.Unmarshal([]byte(pod_annotations["rdma_interfaces_required"]), &interfaces_needed)
			//if the RDMA interface requirements were malformatted,
			//	reject all nodes with an error describing the
			//	problem (this error will show up in the output
			//	for 'kubectl describe pods <pod_name>')
			if(err != nil) {
				log.Println("Pod's RDMA resources request JSON was malformatted.")
				for _, node := range sched_extender_args.Nodes.Items {
					canNotSchedule[node.Name] = "RDMA Scheduler Extension: 'rdma_interfaces_required' field in pod YAML file is malformatted."
				}
			//otherwise, if the RDMA interface requirements were correctly formatted
			} else {
				log.Printf("Pod's RDMA resource requirements: %+v", interfaces_needed)

				//concurrently send a request to the DaemonSet
				//	on each potential node to get information
				//	about what RDMA resources they have available,
				//	then determine if those reqources are enough
				//	to satisfy the pod's request.
				//
				//	results from this will be passed back over the
				//	'node_eligibility_channel'.
				for i, node := range sched_extender_args.Nodes.Items {
				        go queryNode(
						i,
						node.Status.Addresses,
						interfaces_needed,
						node_eligibility_channel,
					)
				}

				//read each of the results from the 'node_eligibility_channel',
				//	then use them to place each potential node in the cluster
				//	into the "can schedule on" or "cannot schedule on" lists
				//	for the pod.
				var cur_elig node_eligibility
				min_cap := math.MaxUint32
				for range sched_extender_args.Nodes.Items {
					cur_elig = <-node_eligibility_channel
					if(cur_elig.capacity < min_cap){
						min_cap = cur_elig.capacity
					}
				}
				log.Println(min_cap)
				log.Println("Results from querying each node:")
				for range sched_extender_args.Nodes.Items {
					//cur_elig = <-node_eligibility_channel
					if cur_elig.enough_resources && cur_elig.capacity == min_cap {
						log.Println("yes")
						log.Println("\t", sched_extender_args.Nodes.Items[cur_elig.index].Name, ": Eligible")
						canSchedule = append(canSchedule, sched_extender_args.Nodes.Items[cur_elig.index])
					} else {
						log.Println("\t", sched_extender_args.Nodes.Items[cur_elig.index].Name, ": Not Eligible")
						canNotSchedule[sched_extender_args.Nodes.Items[cur_elig.index].Name] = cur_elig.ineligibility_reason
					}
				}
				log.Println(min_cap)
			}
		}

		//build a results structure from the lists of nodes that can
		//	and cannot meet the RDMA needs of the pod to be scheduled.
		extender_filter_results = &schedulerapi.ExtenderFilterResult{
			Nodes: &v1.NodeList{
				Items: canSchedule,
			},
			FailedNodes: canNotSchedule,
			Error:       "",
		}
	}

	//serialize the results structure into a response
	response_body, err := json.Marshal(extender_filter_results)
	if(err != nil) {
		panic(err)
	}
	//send the response back to the k8s core scheduler
	response.Write(response_body)
}

// getEnvVar reads the value of an environment variable from the operating,
//     system, returning a specified default value if the variable is empty.
func getEnvVar(var_name string, default_value string) string {
	var_value := os.Getenv(var_name)
	if(var_value == "") {
		var_value = default_value
	}

	return var_value
}


func main() {
	//we will create an HTTP server that listens for queries to a specific URL
	router := httprouter.New()
	router.POST(RdmaSchedulerExtenderHttpListenPath, HandleSchedulerFilterRequest)

	//get the port to listen on from an environment variable, or use default
	port := getEnvVar("PORT", RdmaSchedulerExtenderDefaultPort)

	//listent on specified port
	log.Println("RDMA scheduler extender listening on port: ", port)
	err := http.ListenAndServe(":" + port, router)
	if(err != nil) {
		log.Fatal(err)
	}
}
