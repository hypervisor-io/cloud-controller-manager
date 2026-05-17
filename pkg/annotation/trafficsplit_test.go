package annotation

import "testing"

func TestParseTrafficSplit_Empty(t *testing.T) {
	got, err := ParseTrafficSplit("")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}

func TestParseTrafficSplit_BlueGreen(t *testing.T) {
	in := `[{"service":"app-blue","weight":80},{"service":"app-green","weight":20}]`
	got, err := ParseTrafficSplit(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 children, got %d", len(got))
	}
	if got[0].Service != "app-blue" || got[0].Weight != 80 {
		t.Errorf("entry 0 wrong: %+v", got[0])
	}
	if got[1].Service != "app-green" || got[1].Weight != 20 {
		t.Errorf("entry 1 wrong: %+v", got[1])
	}
}

func TestParseTrafficSplit_MissingService(t *testing.T) {
	_, err := ParseTrafficSplit(`[{"weight":50}]`)
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestParseTrafficSplit_WeightOutOfRange(t *testing.T) {
	cases := []string{
		`[{"service":"a","weight":-1}]`,
		`[{"service":"a","weight":1001}]`,
	}
	for _, c := range cases {
		if _, err := ParseTrafficSplit(c); err == nil {
			t.Errorf("expected error for %s", c)
		}
	}
}

func TestParseTrafficSplit_BadJSON(t *testing.T) {
	if _, err := ParseTrafficSplit(`not-json`); err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestParseRoutingRuleBackends_IgnoresRulesWithoutBackends(t *testing.T) {
	// Existing single-backend rules MUST be passed through (nil result).
	in := `[{"port":443,"match":"sni","value":"web.example.com"}]`
	got, err := ParseRoutingRuleBackends(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil (no backends), got %+v", got)
	}
}

func TestParseRoutingRuleBackends_Canary(t *testing.T) {
	in := `[
		{"port":443,"match":"host","value":"app.example.com","backends":[
			{"service":"app-blue","weight":80},
			{"service":"app-green","weight":20}
		]}
	]`
	got, err := ParseRoutingRuleBackends(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(got))
	}
	s := got[0]
	if s.FrontendPort != 443 {
		t.Errorf("port wrong: %d", s.FrontendPort)
	}
	if s.Match == nil || s.Match.Type != "host" || s.Match.Value != "app.example.com" {
		t.Errorf("match wrong: %+v", s.Match)
	}
	if len(s.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(s.Children))
	}
}

func TestParseRoutingRuleBackends_MixedRulesOnlyEmitBackendOnes(t *testing.T) {
	in := `[
		{"port":443,"match":"sni","value":"web.example.com"},
		{"port":443,"match":"host","value":"canary.example.com","backends":[{"service":"app-canary","weight":100}]}
	]`
	got, err := ParseRoutingRuleBackends(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 spec (only the rule with backends), got %d", len(got))
	}
	if got[0].Match.Value != "canary.example.com" {
		t.Errorf("wrong rule kept: %+v", got[0])
	}
}

func TestParseRoutingRuleBackends_MissingMatch(t *testing.T) {
	in := `[{"port":443,"backends":[{"service":"a","weight":50}]}]`
	if _, err := ParseRoutingRuleBackends(in); err == nil {
		t.Fatal("expected error: backends without match")
	}
}

func TestParseRoutingRuleBackends_BadWeight(t *testing.T) {
	in := `[{"port":443,"match":"host","value":"x","backends":[{"service":"a","weight":99999}]}]`
	if _, err := ParseRoutingRuleBackends(in); err == nil {
		t.Fatal("expected weight range error")
	}
}
