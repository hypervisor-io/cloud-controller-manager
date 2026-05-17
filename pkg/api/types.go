package api

type Port struct {
	Port       int    `json:"port"`
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`
}

type Host struct {
	UUID       string `json:"uuid"`
	InstanceID string `json:"instance_id"`
	InternalIP string `json:"internal_ip"`
}

type LoadBalancer struct {
	ID               string  `json:"id"`
	Status           string  `json:"status"`
	PublicIP         *string `json:"public_ip"`
	Ports            []Port  `json:"ports"`
	Hosts            []Host  `json:"hosts"`
	PlanName         string  `json:"plan_name"`
	ServiceUID       string  `json:"service_uid"`
	ServiceNamespace string  `json:"service_namespace"`
	ServiceName      string  `json:"service_name"`
}

type ServiceCreateRequest struct {
	ServiceUID            string            `json:"service_uid"`
	ServiceNamespace      string            `json:"service_namespace"`
	ServiceName           string            `json:"service_name"`
	Annotations           map[string]string `json:"annotations"`
	Ports                 []Port            `json:"ports"`
	NodeProviderIDs       []string          `json:"node_provider_ids"`
	// External traffic policy: "Cluster" (default, SNAT, no source-IP) or
	// "Local" (route to nodes with local pod only, preserve source IP).
	ExternalTrafficPolicy string `json:"external_traffic_policy,omitempty"`
	// Health-check NodePort populated by K8s when ETP=Local. The master uses
	// this port for haproxy health checks so nodes without a local pod drop
	// out of the rotation automatically (kube-proxy returns 503 there).
	HealthCheckNodePort   int    `json:"health_check_node_port,omitempty"`
}

type ServiceUpdateRequest struct {
	Ports                 []Port            `json:"ports,omitempty"`
	Annotations           map[string]string `json:"annotations,omitempty"`
	NodeProviderIDs       []string          `json:"node_provider_ids,omitempty"`
	ExternalTrafficPolicy string            `json:"external_traffic_policy,omitempty"`
	HealthCheckNodePort   int               `json:"health_check_node_port,omitempty"`
}

type SyncHostsRequest struct {
	Assign []string `json:"assign"`
	Remove []string `json:"remove"`
}

type Address struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type InstanceMetadata struct {
	ProviderID   string    `json:"provider_id"`
	InstanceType string    `json:"instance_type"`
	Region       string    `json:"region"`
	Addresses    []Address `json:"addresses"`
	Exists       bool      `json:"exists"`
	Shutdown     bool      `json:"shutdown"`
}

// TrafficSplitEntry is a single weighted backend in a traffic-split rule.
// The CCM resolves the child Service to NodePort + (optional) health-check
// NodePort, and passes that to the platform so HAProxy can target nodes
// directly without going back through kube-proxy.
type TrafficSplitEntry struct {
	RefName             string `json:"ref_name"`
	RefNamespace        string `json:"ref_namespace"`
	Weight              int    `json:"weight"`
	NodePort            int    `json:"node_port"`
	HealthCheckNodePort int    `json:"health_check_node_port,omitempty"`
}

// MatchSpec describes a routing predicate for a per-rule traffic split
// (e.g. SNI host, path prefix). Nil means "default backend for the
// frontend port".
type MatchSpec struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// TrafficSplitRequest is the body sent to PATCH /lb/service/{id}/traffic-split.
// Entries[].Weight is relative (HAProxy normalizes to total).
type TrafficSplitRequest struct {
	Entries      []TrafficSplitEntry `json:"entries"`
	FrontendPort int                 `json:"frontend_port"`
	Match        *MatchSpec          `json:"match,omitempty"`
}

type AnnotationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}

func (e *AnnotationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}
