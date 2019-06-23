package main

import (
	"encoding/json"
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
			log.Print("\t", node.Name)

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
