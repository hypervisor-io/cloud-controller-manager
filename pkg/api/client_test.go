package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_SendsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: NewFakeTokenLoader("abc"), HTTPClient: http.DefaultClient}

	_, _ = c.GetLoadBalancer("svc-uid-1")
	if !strings.HasPrefix(gotAuth, "Bearer abc") {
		t.Errorf("Authorization header missing or wrong: %q", gotAuth)
	}
}

func TestClient_GetLoadBalancer_404Returns_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: NewFakeTokenLoader("abc"), HTTPClient: http.DefaultClient}
	lb, err := c.GetLoadBalancer("svc-x")
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if lb != nil {
		t.Errorf("expected nil lb on 404")
	}
}

func TestClient_CreateLoadBalancer_PostsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ServiceCreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ServiceUID != "svc-c" {
			t.Errorf("expected svc-c, got %q", req.ServiceUID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(LoadBalancer{ID: "lb-1", Status: "provisioning", ServiceUID: req.ServiceUID})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: NewFakeTokenLoader("abc"), HTTPClient: http.DefaultClient}
	lb, err := c.CreateLoadBalancer(ServiceCreateRequest{ServiceUID: "svc-c"})
	if err != nil {
		t.Fatal(err)
	}
	if lb.ID != "lb-1" {
		t.Errorf("got id %q", lb.ID)
	}
}

func TestClient_CreateLoadBalancer_422AnnotationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(AnnotationError{Code: "missing_annotation", Field: "plan"})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: NewFakeTokenLoader("abc"), HTTPClient: http.DefaultClient}
	_, err := c.CreateLoadBalancer(ServiceCreateRequest{ServiceUID: "svc-bad"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing_annotation") {
		t.Errorf("expected error mentioning missing_annotation, got %v", err)
	}
}
