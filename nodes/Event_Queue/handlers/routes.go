package main

import (
	"fmt"
	"net/http"
)

func EventTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

func AddServiceHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Adding service...")
}

func AddQueueHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Adding queue...")
}

func GetQueueHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Getting queue...")
}

func GetServiceHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Getting service...")
}

/*
Workflow for removing services:
1. Service signals intent to delete (marks itself as "draining")
2. MQ server stops routing new messages to that service's queues
3. Queues finish processing or dead-letter remaining messages
4. MQ server deletes the queues and notifies the service
5. Service shuts down

same thing for queues
*/

func RemoveQueueHandler(ServiceId string, QueueId string) {
	fmt.Println("Removing queue...")
}

func RemoveServiceHandler(ServiceId string) {
	fmt.Println("Removing service...")
}
