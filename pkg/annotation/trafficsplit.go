// Package annotation parses Hypervisor.io LoadBalancer Service annotations
// that require client-side resolution (e.g. traffic-split needs to look up
// child Service NodePorts before the master can be PATCHed).
//
// Simple text annotations (plan, source-ranges, etc.) are forwarded as-is
// in Annotations on ServiceCreate/UpdateRequest; this package is reserved
// for annotations whose payload references other Kubernetes objects.
package annotation

import (
	"encoding/json"
	"fmt"
)

// TrafficSplitChild is one child Service reference inside a split.
type TrafficSplitChild struct {
	// Service is the child Service name (required).
	Service string `json:"service"`
	// Namespace is the child Service namespace. Empty string means
	// "same namespace as parent" — resolved by the caller.
	Namespace string `json:"namespace,omitempty"`
	// Weight is the integer relative weight passed through to HAProxy.
	// Must be 0-1000 (HAProxy upper bound). Sum need not equal 100.
	Weight int `json:"weight"`
}

// TrafficSplitSpec is the parsed form of either the standalone
// `traffic-split` annotation (Match == nil) or one rule's `backends`
// extension under `routing-rules` (Match != nil).
type TrafficSplitSpec struct {
	// FrontendPort is the parent LB frontend port the split applies to.
	// For the standalone annotation, the caller fans this out across
	// every parent Service port (one PATCH per port).
	FrontendPort int
	// Match is nil for the catch-all split (annotation form) and set when
	// the spec came from a routing-rules entry with a backends array.
	Match *MatchSpec
	// Children are the resolved child Service refs + weights, in input
	// order. NodePort resolution happens later via the informer cache.
	Children []TrafficSplitChild
}

// MatchSpec mirrors api.MatchSpec but lives here to keep the annotation
// parser free of api package coupling. The reconciler converts.
type MatchSpec struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// rawSplitEntry is the on-wire shape of the standalone annotation, e.g.
//
//	[{"service":"app-blue","weight":80}, ...]
type rawSplitEntry struct {
	Service   string `json:"service"`
	Namespace string `json:"namespace,omitempty"`
	Weight    int    `json:"weight"`
}

// rawRoutingRule is the on-wire shape of one routing-rules entry. The
// existing CCM does not parse routing-rules client-side (the master does
// the heavy lifting); we only parse it here to surface the optional
// `backends` array, which the master cannot resolve on its own because
// node-port discovery happens via the K8s informer cache.
type rawRoutingRule struct {
	Port     int             `json:"port"`
	Match    string          `json:"match"`
	Value    string          `json:"value"`
	Backends []rawSplitEntry `json:"backends,omitempty"`
}

// ParseTrafficSplit reads the standalone `traffic-split` annotation
// (without the prefix — caller pre-strips). Returns nil, nil when the
// key is absent. Returns an error on malformed JSON or invalid weights.
func ParseTrafficSplit(rawJSON string) ([]TrafficSplitChild, error) {
	if rawJSON == "" {
		return nil, nil
	}
	var entries []rawSplitEntry
	if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
		return nil, fmt.Errorf("traffic-split: invalid JSON: %w", err)
	}
	out := make([]TrafficSplitChild, 0, len(entries))
	for i, e := range entries {
		if e.Service == "" {
			return nil, fmt.Errorf("traffic-split[%d]: missing service", i)
		}
		if e.Weight < 0 || e.Weight > 1000 {
			return nil, fmt.Errorf("traffic-split[%d]: weight %d out of range [0,1000]", i, e.Weight)
		}
		out = append(out, TrafficSplitChild{
			Service:   e.Service,
			Namespace: e.Namespace,
			Weight:    e.Weight,
		})
	}
	return out, nil
}

// ParseRoutingRuleBackends parses the `routing-rules` annotation and
// returns one TrafficSplitSpec per rule that carries a `backends` array.
// Rules without `backends` are ignored here (those continue through the
// existing master-side single-backend path). Returns nil, nil when the
// annotation is absent or empty.
func ParseRoutingRuleBackends(rawJSON string) ([]TrafficSplitSpec, error) {
	if rawJSON == "" {
		return nil, nil
	}
	var rules []rawRoutingRule
	if err := json.Unmarshal([]byte(rawJSON), &rules); err != nil {
		return nil, fmt.Errorf("routing-rules: invalid JSON: %w", err)
	}
	var out []TrafficSplitSpec
	for i, r := range rules {
		if len(r.Backends) == 0 {
			continue
		}
		if r.Port == 0 {
			return nil, fmt.Errorf("routing-rules[%d]: missing port", i)
		}
		if r.Match == "" || r.Value == "" {
			return nil, fmt.Errorf("routing-rules[%d]: backends require match+value", i)
		}
		children := make([]TrafficSplitChild, 0, len(r.Backends))
		for j, b := range r.Backends {
			if b.Service == "" {
				return nil, fmt.Errorf("routing-rules[%d].backends[%d]: missing service", i, j)
			}
			if b.Weight < 0 || b.Weight > 1000 {
				return nil, fmt.Errorf("routing-rules[%d].backends[%d]: weight %d out of range [0,1000]", i, j, b.Weight)
			}
			children = append(children, TrafficSplitChild{
				Service:   b.Service,
				Namespace: b.Namespace,
				Weight:    b.Weight,
			})
		}
		out = append(out, TrafficSplitSpec{
			FrontendPort: r.Port,
			Match:        &MatchSpec{Type: r.Match, Value: r.Value},
			Children:     children,
		})
	}
	return out, nil
}
