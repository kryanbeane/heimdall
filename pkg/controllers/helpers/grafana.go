package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
)

type OrgID struct {
	OrgID int `json:"orgId"`
}

type APIKey struct {
	Key string `json:"key"`
}

func CreateGrafanaOrg(grafanaURL, grafanaUser, grafanaPassword string) (OrgID, error) {
	// Check if the organization already exists
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/orgs/name/%s", grafanaURL, "heimdall-org"), nil)
	if err != nil {
		return OrgID{OrgID: 0}, err
	}

	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err := client.Do(req)
	if err != nil {
		return OrgID{OrgID: 0}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var org struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
			return OrgID{OrgID: 0}, err
		}

		return OrgID{OrgID: org.ID}, nil
	} else if resp.StatusCode != http.StatusNotFound {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logrus.Errorf("failed to check for existing organization. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
		return OrgID{OrgID: 0}, err
	}

	// If the organization doesn't exist, create it
	org := map[string]interface{}{
		"name": "heimdall-org",
	}

	orgJSON, err := json.Marshal(org)
	if err != nil {
		return OrgID{OrgID: 0}, err
	}

	req, err = http.NewRequest("POST", grafanaURL+"/api/orgs", bytes.NewBuffer(orgJSON))
	if err != nil {
		return OrgID{OrgID: 0}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err = client.Do(req)
	if err != nil {
		return OrgID{OrgID: 0}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logrus.Errorf("failed to create Grafana organization. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
		return OrgID{OrgID: 0}, err
	}

	// Extract the organization ID from the JSON response
	var orgID struct {
		OrgID int `json:"orgId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orgID); err != nil {
		return OrgID{OrgID: 0}, err
	}

	return OrgID{OrgID: orgID.OrgID}, nil
}

func AddAdminUserToOrg(grafanaURL, grafanaUser, grafanaPassword string, orgID OrgID) error {
	// Create HTTP client with basic authentication
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/orgs/%d/users", grafanaURL, orgID.OrgID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get users for organization. Status: %d", resp.StatusCode)
	}

	var users []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return fmt.Errorf("failed to decode users JSON: %v", err)
	}

	// Check if admin user already exists in the organization
	for _, user := range users {
		if user["login"] == "admin" {
			return nil
		}
	}

	// Add the admin user to the organization
	user := map[string]interface{}{
		"name":     "Admin",
		"login":    "admin",
		"email":    "admin@localhost",
		"password": "",
	}

	userJSON, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user JSON: %v", err)
	}

	req, err = http.NewRequest("POST", fmt.Sprintf("%s/api/orgs/%d/users", grafanaURL, orgID.OrgID), bytes.NewBuffer(userJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("failed to add admin user to organization. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
	}

	// Switch the org context for the admin user
	req, err = http.NewRequest("POST", fmt.Sprintf("%s/api/user/using/%d", grafanaURL, orgID.OrgID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("failed to switch org context for admin user. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func CreateGrafanaAPIToken(grafanaURL, grafanaUser, grafanaPassword string) (APIKey, error) {
	// Check if API token already exists
	client := &http.Client{}

	randomTokenName, _ := uuid.NewUUID()

	// Create an API token if it doesn't already exist
	apiToken := map[string]interface{}{
		"name": randomTokenName,
		"role": "Admin",
	}

	apiTokenJSON, err := json.Marshal(apiToken)
	if err != nil {
		return APIKey{Key: ""}, fmt.Errorf("failed to marshal API token JSON: %v", err)
	}

	req, err := http.NewRequest("POST", grafanaURL+"/api/auth/keys", bytes.NewBuffer(apiTokenJSON))
	if err != nil {
		return APIKey{Key: ""}, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err := client.Do(req)
	if err != nil {
		return APIKey{Key: ""}, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return APIKey{Key: ""}, fmt.Errorf("failed to create Grafana API token. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
	}

	// Extract the API key from the JSON response
	var apiKey struct {
		Key string `json:"key"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiKey); err != nil {
		return APIKey{Key: ""}, fmt.Errorf("failed to decode API key: %v", err)
	}

	return APIKey{Key: apiKey.Key}, nil
}

var dashboard = map[string]interface{}{
	"title":         "Heimdall Overview",
	"tags":          []string{"kubernetes", "prometheus"},
	"timezone":      "browser",
	"schemaVersion": 16,
	"version":       0,
	"refresh":       "5s",
	"panels": []map[string]interface{}{
		{
			"title": "Heimdall Controller CPU Usage",
			"type":  "graph",
			"gridPos": map[string]int{
				"x": 0,
				"y": 0,
				"w": 9,
				"h": 8,
			},
			"targets": []map[string]string{
				{
					"expr": `sum by (pod) (
  rate(container_cpu_usage_seconds_total{namespace="heimdall", pod=~"heimdall-controller-.*"}[5m]) * 100
) * on(pod) group_left()
  (kube_pod_status_ready{namespace="heimdall", pod=~"heimdall-controller-.*", condition="true"})`,
				},
			},
		},
		{
			"title": "Heimdall Admission Controller CPU Usage",
			"type":  "graph",
			"gridPos": map[string]int{
				"x": 9,
				"y": 0,
				"w": 9,
				"h": 8,
			},
			"targets": []map[string]string{
				{
					"expr": `sum by (pod) (
  rate(container_cpu_usage_seconds_total{namespace="heimdall", pod=~"heimdall-admission-controller-.*"}[5m]) * 100
) * on(pod) group_left()
  (kube_pod_status_ready{namespace="heimdall", pod=~"heimdall-admission-controller-.*", condition="true"})`,
				},
			},
		},
		{
			"title": "Heimdall Controller Memory Usage",
			"type":  "graph",
			"gridPos": map[string]int{
				"x": 0,
				"y": 8,
				"w": 9,
				"h": 8,
			},
			"targets": []map[string]string{
				{
					"expr": `sum by (pod) (
  container_memory_usage_bytes{namespace="heimdall", pod=~"heimdall-controller-.*", container!="POD"}
) * on(pod) group_left()
  (kube_pod_status_ready{namespace="heimdall", pod=~"heimdall-controller-.*", condition="true"})`,
				},
			},
		},
		{
			"title": "Heimdall Admission Controller Memory Usage",
			"type":  "graph",
			"gridPos": map[string]int{
				"x": 9,
				"y": 8,
				"w": 9,
				"h": 8,
			},
			"targets": []map[string]string{
				{
					"expr": `sum by (pod) (
  container_memory_usage_bytes{namespace="heimdall", pod=~"heimdall-admission-controller-.*", container!="POD"}
) * on(pod) group_left()
  (kube_pod_status_ready{namespace="heimdall", pod=~
  "heimdall-admission-controller-.*", condition="true"})`,
				},
			},
		},
		{
			"title": "Total Number of Pods (Heimdall)",
			"type":  "stat",
			"gridPos": map[string]int{
				"x": 18,
				"y": 0,
				"w": 6,
				"h": 8,
			},
			"targets": []map[string]string{
				{
					"expr": `count(kube_pod_status_ready{namespace="heimdall", condition="true"})`,
				},
			},
			"format":          "short",
			"valueName":       "current",
			"prefix":          "",
			"postfix":         "",
			"thresholds":      "0,50,100",
			"colorBackground": true,
			"colorValue":      true,
			"colors": []map[string]interface{}{
				{
					"value": "null",
					"color": "dark-gray",
				},
				{
					"value": "0",
					"color": "green",
				},
				{
					"value": "50",
					"color": "orange",
				},
				{
					"value": "100",
					"color": "red",
				},
			},
		},
		{
			"title": "Total Pod Restarts (Heimdall, Last Hour)",
			"type":  "stat",
			"gridPos": map[string]int{
				"x": 18,
				"y": 8,
				"w": 6,
				"h": 8,
			},
			"targets": []map[string]string{
				{
					"expr": `sum(increase(kube_pod_container_status_restarts_total{namespace="heimdall"}[1h]))`,
				},
			},
			"format":          "short",
			"valueName":       "current",
			"prefix":          "",
			"postfix":         "",
			"thresholds":      "0,50,100",
			"colorBackground": true,
			"colorValue":      true,
			"colors": []map[string]interface{}{
				{
					"value": "null",
					"color": "dark-gray",
				},
				{
					"value": "0",
					"color": "green",
				},
				{
					"value": "50",
					"color": "orange",
				},
				{
					"value": "100",
					"color": "red",
				},
			},
		},
	},
}

func CreateGrafanaDashboard(grafanaURL string, apiKey APIKey, folderId int) error {
	// Define the dashboard data
	client := &http.Client{}

	dashboardPayload := map[string]interface{}{
		"dashboard": dashboard,
		"folderId":  folderId,
		"overwrite": false,
	}

	dashboardJSON, err := json.Marshal(dashboardPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal dashboard JSON: %v", err)
	}

	// Add a dashboard using the API key
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/dashboards/db", grafanaURL), bytes.NewBuffer(dashboardJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("failed to create Grafana dashboard. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
	}

	if resp.StatusCode == http.StatusConflict {
		return nil
	}

	return nil
}

func GetOrCreateGrafanaFolder(grafanaURL, grafanaUser, grafanaPassword, folderName string) (int, error) {
	// Create HTTP client with basic authentication
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/search?type=dash-folder", grafanaURL), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %v", err)
	}

	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to get folders. Status: %d", resp.StatusCode)
	}

	var folders []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&folders); err != nil {
		return 0, fmt.Errorf("failed to decode folders JSON: %v", err)
	}

	// Check if folder already exists
	for _, folder := range folders {
		if folder["title"] == folderName {
			return int(folder["id"].(float64)), nil
		}
	}

	// Create new folder
	newFolder := map[string]string{
		"title": folderName,
	}
	newFolderJSON, err := json.Marshal(newFolder)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal new folder JSON: %v", err)
	}

	req, err = http.NewRequest("POST", fmt.Sprintf("%s/api/folders", grafanaURL), bytes.NewBuffer(newFolderJSON))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %v", err)
	}

	req.SetBasicAuth(grafanaUser, grafanaPassword)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to create Grafana folder. Status: %d", resp.StatusCode)
	}

	var createdFolder map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&createdFolder); err != nil {
		return 0, fmt.Errorf("failed to decode created folder JSON: %v", err)
	}

	return int(createdFolder["id"].(float64)), nil
}
