package controllers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	v1 "k8s.io/api/admission/v1"
	v12 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"net/http"
)

// AdmissionController is a struct that holds the configuration for the admission controller.
type AdmissionController struct {
	client kubernetes.Interface
}

// handleAdmissionRequest is a function that handles the admission request.
func (a *AdmissionController) handleAdmissionRequest(w http.ResponseWriter, r *http.Request) {
	// Read the admission request from the request body.
	requestBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request: %v", err), http.StatusBadRequest)
		return
	}

	// Decode the admission request into an AdmissionReview object.
	admissionReview := v1.AdmissionReview{}
	if _, _, err := scheme.Codecs.UniversalDeserializer().Decode(requestBytes, nil, &admissionReview); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	// Retrieve the object being modified from the admission request.
	rawObject := admissionReview.Request.Object

	// Decode the raw object into the appropriate Kubernetes object type.
	object, _, err := scheme.Codecs.UniversalDeserializer().Decode(rawObject.Raw, nil, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode object: %v", err), http.StatusBadRequest)
		return
	}

	// Check that the object is of the correct type.
	deployment, ok := object.(*v12.Deployment)
	if !ok {
		http.Error(w, "Object is not a Deployment", http.StatusBadRequest)
		return
	}

	// Check if the deployment has the "can-edit" label.
	labels := deployment.GetLabels()
	if labels == nil || labels["can-edit"] != "true" {
		http.Error(w, "Deployment does not have the 'can-edit' label", http.StatusBadRequest)
		return
	}

	// Check the source of the request.
	requestSource := admissionReview.Request.UserInfo.Username
	if requestSource != "system:serviceaccount:my-namespace:my-operator" {
		http.Error(w, "Request is not from the 'my-operator' service account", http.StatusBadRequest)
		return
	}

	// If we got here, the request is allowed, so return an empty response.
	admissionResponse := v1.AdmissionResponse{
		Allowed: true,
	}
	admissionReview.Response = &admissionResponse

	responseBytes, err := json.Marshal(admissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(responseBytes); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write response: %v", err), http.StatusInternalServerError)
		return
	}
}

func main() {
	var config *rest.Config
	var err error

	// Load the Kubernetes configuration from the default location.
	// You can also provide a path to a kubeconfig file using the KUBECONFIG environment variable.
	config, err = rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	// Create a Kubernetes clientset.
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Create the admission controller.
	admissionController := AdmissionController{
		client: clientset,
	}

	// Register the handler for the admission webhook.
	http.HandleFunc("/mutate", admissionController.handleAdmissionRequest)

	// Start the server.
	if err := http.ListenAndServeTLS(":8443", "/var/run/secrets/tls/cert.pem", "/var/run/secrets/tls/key.pem", nil); err != nil {
		panic(err.Error())
	}
}
