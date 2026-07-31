package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

type Client struct {
	BaseURL    string
	ClusterID  string
	Token      *TokenLoader
	HTTPClient *http.Client
}

func (c *Client) clusterURL(path string) string {
	return fmt.Sprintf("%s/cluster/%s%s", c.BaseURL, c.ClusterID, path)
}

func (c *Client) request(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.clusterURL(path), bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token.Token())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	return c.HTTPClient.Do(req)
}

func (c *Client) GetLoadBalancer(serviceUID string) (*LoadBalancer, error) {
	q := url.Values{"uid": []string{serviceUID}}
	resp, err := c.request("GET", "/lb/service?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, decodeAnnotationError(resp)
	}
	var lb LoadBalancer
	if err := json.NewDecoder(resp.Body).Decode(&lb); err != nil {
		return nil, err
	}
	return &lb, nil
}

func (c *Client) CreateLoadBalancer(req ServiceCreateRequest) (*LoadBalancer, error) {
	resp, err := c.request("POST", "/lb/service", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, decodeAnnotationError(resp)
	}
	var lb LoadBalancer
	if err := json.NewDecoder(resp.Body).Decode(&lb); err != nil {
		return nil, err
	}
	return &lb, nil
}

func (c *Client) UpdateLoadBalancer(lbID string, req ServiceUpdateRequest) (*LoadBalancer, error) {
	resp, err := c.request("PATCH", "/lb/service/"+lbID, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, decodeAnnotationError(resp)
	}
	var lb LoadBalancer
	if err := json.NewDecoder(resp.Body).Decode(&lb); err != nil {
		return nil, err
	}
	return &lb, nil
}

func (c *Client) SyncHosts(lbID string, req SyncHostsRequest) ([]Host, error) {
	resp, err := c.request("PATCH", "/lb/service/"+lbID+"/hosts", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, decodeAnnotationError(resp)
	}
	var body struct {
		Hosts []Host `json:"hosts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Hosts, nil
}

// SyncTrafficSplit PATCHes the traffic-split overlay for lbID. The master
// endpoint currently returns 501 (not implemented — see
// ClusterControllerLoadBalancerController::trafficSplit in the Master repo);
// this method exists so the CCM module compiles against the design-doc
// contract (§3.2) and is ready once the master-side bridge lands. It is not
// called by any running controller yet.
func (c *Client) SyncTrafficSplit(lbID string, req TrafficSplitRequest) (*TrafficSplitResponse, error) {
	resp, err := c.request("PATCH", "/lb/service/"+lbID+"/traffic-split", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, decodeAnnotationError(resp)
	}
	var out TrafficSplitResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteLoadBalancer(lbID string) error {
	resp, err := c.request("DELETE", "/lb/service/"+lbID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 || resp.StatusCode == 404 {
		return nil
	}
	return decodeAnnotationError(resp)
}

func (c *Client) GetInstance(providerID string) (*InstanceMetadata, error) {
	q := url.Values{"provider_id": []string{providerID}}
	resp, err := c.request("GET", "/instance?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, decodeAnnotationError(resp)
	}
	var im InstanceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&im); err != nil {
		return nil, err
	}
	return &im, nil
}

func decodeAnnotationError(resp *http.Response) error {
	var e AnnotationError
	if err := json.NewDecoder(resp.Body).Decode(&e); err == nil && e.Code != "" {
		return &e
	}
	return fmt.Errorf("master api error: %s", resp.Status)
}
