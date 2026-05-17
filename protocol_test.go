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
	a := map[string]string{
		"service.beta.kubernetes.io/hypervisor-loadbalancer-plan":   "std-1g",
		"service.beta.kubernetes.io/hypervisor-loadbalancer-public": "true",
	}
	if v, _ := AnnotationGet(a, "plan"); v != "std-1g" {
		t.Errorf("expected plan=std-1g, got %q", v)
	}
	if v, _ := AnnotationGet(a, "missing"); v != "" {
		t.Errorf("expected empty string for missing key, got %q", v)
	}
}
