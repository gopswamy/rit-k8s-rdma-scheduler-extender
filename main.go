package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/julienschmidt/httprouter"

	"k8s.io/api/core/v1"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
)


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

		log.Print("NODE NAMES:")

	        for _, node := range sched_extender_args.Nodes.Items {
			var addr_to_dial = ""
			for _, node_addr := range node.Status.Addresses {
				if((node_addr.Type == v1.NodeInternalIP) || (node_addr.Type == v1.NodeInternalDNS)) {
					addr_to_dial = node_addr.Address
					break
				}
			}

			log.Print("\t", node.Name, ": ", node.Status.Addresses)
			log.Print("\t\tAddr to dial: ", addr_to_dial)

			resp, err := http.Get(fmt.Sprintf("http://%s:%s/getpfs", addr_to_dial, "54005"))
			if(err != nil) {
				canNotSchedule[node.Name] = "SCHEDULER EXTENDER SAYS: May not scheduler pod on this node."
				continue
			}

			data, err := ioutil.ReadAll(resp.Body)
			if(err != nil) {
				canNotSchedule[node.Name] = "SCHEDULER EXTENDER SAYS: May not scheduler pod on this node."
				continue
			}

			var pfs []*PF
			err = json.Unmarshal(data, &pfs)
			if(err != nil) {
				canNotSchedule[node.Name] = "SCHEDULER EXTENDER SAYS: May not scheduler pod on this node."
				continue
			}

			log.Print("\t\tPFs on node:")
			for _, pf := range pfs {
				log.Print("\t\t\t", pf)
			}

			if(node.Name == "ty") {
	                        canSchedule = append(canSchedule, node)
			} else {
				canNotSchedule[node.Name] = "SCHEDULER EXTENDER SAYS: May not scheduler pod on this node."
			}
	        }

	        extender_filter_results = &schedulerapi.ExtenderFilterResult{
	                Nodes: &v1.NodeList{
	                        Items: canSchedule,
	                },
	                FailedNodes: canNotSchedule,
	                Error:       "",
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
