package main

/*
TODO:
	-add error checking for un-annotated pods
*/


import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"

	"github.com/cal8384/k8s-rdma-common/rdma_hardware_info"
	"github.com/cal8384/k8s-rdma-common/knapsack_pod_placement"

	"k8s.io/api/core/v1"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
)

type node_eligibility struct {
        index int
        enough_resources bool
	ineligibility_reason string
}

func queryNode(node_index int, node_addresses []v1.NodeAddress, needed_resources []knapsack_pod_placement.RdmaInterfaceRequest, output_channel chan<- node_eligibility) {
	var node_result node_eligibility
	node_result.index = node_index

	for _, node_addr := range node_addresses {
		if((node_addr.Type == v1.NodeInternalIP) || (node_addr.Type == v1.NodeInternalDNS)) {
			http_client := http.Client {
				Timeout: time.Duration(1500 * time.Millisecond),
			}

			resp, err := http_client.Get(fmt.Sprintf("http://%s:%s/getpfs", node_addr.Address, "54005"))
			if(err != nil) {
				continue
			}

			data, err := ioutil.ReadAll(resp.Body)
			if(err != nil) {
				continue
			}

			var pfs []rdma_hardware_info.PF
			err = json.Unmarshal(data, &pfs)
			if(err != nil) {
				continue
			}

			placement, placement_success := knapsack_pod_placement.PlacePod(needed_resources, pfs)

			if(!placement_success) {
				log.Println("No Possible Placement: ", node_addr)
				node_result.enough_resources = false
				node_result.ineligibility_reason = "RDMA Scheduler Extension: Node did not have enough free RDMA resources."
				output_channel <- node_result
				return
			} else {
				log.Println("Possible Placement: ", node_addr, ": ", placement)
				node_result.enough_resources = true
				node_result.ineligibility_reason = ""
				output_channel <- node_result
				return
			}
		}
	}

	node_result.enough_resources = false
	node_result.ineligibility_reason = "RDMA Scheduler Extension: Unable to collect information on available RDMA resources for node."
	output_channel <- node_result
	return
}


func HandleSchedulerFilterRequest(response http.ResponseWriter, request *http.Request, _ httprouter.Params) {
        if(request.Body == nil) {
                http.Error(response, "Request body was empty.", 400)
                return
        }

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)

        var sched_extender_args schedulerapi.ExtenderArgs
	var extender_filter_results *schedulerapi.ExtenderFilterResult

	err := json.NewDecoder(request.Body).Decode(&sched_extender_args)
	if(err != nil) {
                extender_filter_results = &schedulerapi.ExtenderFilterResult{
                        Nodes:       nil,
                        FailedNodes: nil,
                        Error:       err.Error(),
                }
	} else {
       		canSchedule := make([]v1.Node, 0, len(sched_extender_args.Nodes.Items))
	        canNotSchedule := make(map[string]string)

		node_eligibility_channel := make(chan node_eligibility)

		pod_annotations := sched_extender_args.Pod.ObjectMeta.Annotations
		//if the pod does not require any RDMA interfaces
		if(pod_annotations["rdma_interfaces_required"] == "") {
			for _, node := range sched_extender_args.Nodes.Items {
				canSchedule = append(canSchedule, node)
			}
		}
		else {

			var interfaces_needed []knapsack_pod_placement.RdmaInterfaceRequest
			err = json.Unmarshal([]byte(pod_annotations["rdma_interfaces_required"]), &interfaces_needed)
			if(err != nil) {
			        for _, node := range sched_extender_args.Nodes.Items {
					canNotSchedule[node.Name] = "RDMA Scheduler Extension: 'rdma_interfaces_required' field in pod YAML file is malformatted."
				}
			} else {
			        for i, node := range sched_extender_args.Nodes.Items {
					go queryNode(
						i,
						node.Status.Addresses,
						interfaces_needed,
						node_eligibility_channel
					)
				}

				var cur_elig node_eligibility
			        for _, _ = range sched_extender_args.Nodes.Items {
					cur_elig = <-node_eligibility_channel
					if(cur_elig.enough_resources) {
						canSchedule = append(canSchedule, sched_extender_args.Nodes.Items[cur_elig.index])
					} else {
						canNotSchedule[sched_extender_args.Nodes.Items[cur_elig.index].Name] = cur_elig.ineligibility_reason
					}
				}
			}
		}

	        extender_filter_results = &schedulerapi.ExtenderFilterResult{
	                Nodes: &v1.NodeList{
	                        Items: canSchedule,
	                },
	                FailedNodes: canNotSchedule,
	                Error:       "",
	        }

		log.Print("NODE NAMES:")
	        for _, node := range sched_extender_args.Nodes.Items {
			log.Print("\t", node.Name, ": ", node.Status.Addresses)
		}
	}

	response_body, err := json.Marshal(extender_filter_results)
	if(err != nil) {
		panic(err)
	}
	response.Write(response_body)
}


func main() {
	router := httprouter.New()

	router.POST("/scheduler/predicates/always_true", HandleSchedulerFilterRequest)

	log.Print("Scheduler extender listening on port: 8888")
	err := http.ListenAndServe(":8888", router)
	if(err != nil) {
		log.Fatal(err)
	}
}
