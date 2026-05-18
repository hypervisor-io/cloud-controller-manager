package hypervisor

import (
	"os"
	"regexp"
)

const (
	ProviderName = "external-hypervisor"

	// defaultAnnotationPrefix matches the master's default
	// `kubernetes.annotation_prefix` config (see config/kubernetes.php).
	// Operators that white-label by setting K8S_ANNOTATION_PREFIX on master
	// must also export ANNOTATION_PREFIX on the CCM Deployment so both ends
	// agree on the key, otherwise CCM-side annotation parsers (traffic-split,
	// routing-rules) silently skip the Service.
	defaultAnnotationPrefix = "service.beta.kubernetes.io/managed-loadbalancer-"
)

// AnnotationPrefix is read from the ANNOTATION_PREFIX env var at process start,
// falling back to defaultAnnotationPrefix. It's a var (not const) so callers
// can override in tests; production code treats it as read-only.
var AnnotationPrefix = func() string {
	if v := os.Getenv("ANNOTATION_PREFIX"); v != "" {
		return v
	}
	return defaultAnnotationPrefix
}()

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
