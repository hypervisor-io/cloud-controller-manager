package hypervisor

import (
	"regexp"
)

const (
	ProviderName     = "external-hypervisor"
	AnnotationPrefix = "service.beta.kubernetes.io/hypervisor-loadbalancer-"
)

var providerIDRegex = regexp.MustCompile(`^hypervisor:///([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// ParseProviderID extracts the UUID from a provider ID of the form
// "hypervisor:///<uuid>". Returns ("", false) for any other scheme or malformed UUID.
func ParseProviderID(s string) (string, bool) {
	m := providerIDRegex.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// AnnotationGet retrieves a Hypervisor.io LB annotation by short key
// (e.g. "plan", "public", "source-ranges"). Returns ("", false) if absent.
func AnnotationGet(annotations map[string]string, key string) (string, bool) {
	if annotations == nil {
		return "", false
	}
	v, ok := annotations[AnnotationPrefix+key]
	return v, ok
}
