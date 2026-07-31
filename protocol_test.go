package hypervisor

import "testing"

func TestParseProviderID(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"hypervisor:///11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111", true},
		{"hypervisor:///not-a-uuid", "", false},
		{"aws:///foo", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ParseProviderID(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseProviderID(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestAnnotationGet(t *testing.T) {
	// Build the keys from AnnotationPrefix rather than restating it. The
	// literal previously hardcoded here ("...hypervisor-loadbalancer-") had
	// diverged from the prefix the code actually uses, which is the master's
	// configured default ("...managed-loadbalancer-", see
	// config/kubernetes.php and K8sLoadBalancerBridgeService). The test could
	// not have passed - and never ran, because this package did not compile.
	a := map[string]string{
		AnnotationPrefix + "plan":   "std-1g",
		AnnotationPrefix + "public": "true",
	}
	if v, _ := AnnotationGet(a, "plan"); v != "std-1g" {
		t.Errorf("expected plan=std-1g, got %q", v)
	}
	if v, _ := AnnotationGet(a, "missing"); v != "" {
		t.Errorf("expected empty string for missing key, got %q", v)
	}
}
