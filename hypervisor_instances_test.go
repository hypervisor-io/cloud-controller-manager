package hypervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hypervisor-io/cloud-controller-manager/pkg/api"
)

func TestInstanceMetadata_ParsesMasterResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.InstanceMetadata{
			ProviderID: "hypervisor:///uuid-1", InstanceType: "small", Region: "hg-eu-west-1",
			Addresses: []api.Address{{Type: "InternalIP", Address: "10.0.1.10"}},
			Exists:    true, Shutdown: false,
		})
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	inst := newInstances(client)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}, Spec: corev1.NodeSpec{ProviderID: "hypervisor:///uuid-1"}}

	md, err := inst.InstanceMetadata(context.TODO(), node)
	if err != nil {
		t.Fatal(err)
	}
	if md.InstanceType != "small" {
		t.Errorf("got %q", md.InstanceType)
	}
	if md.Region != "hg-eu-west-1" {
		t.Errorf("got %q", md.Region)
	}
}

func TestInstanceExists_Handles404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	inst := newInstances(client)
	node := &corev1.Node{Spec: corev1.NodeSpec{ProviderID: "hypervisor:///99999999-9999-9999-9999-999999999999"}}
	exists, err := inst.InstanceExists(context.TODO(), node)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Errorf("expected exists=false on 404")
	}
}

func TestInstanceShutdown_ReflectsField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.InstanceMetadata{Exists: true, Shutdown: true})
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, ClusterID: "cid-1", Token: api.NewFakeTokenLoader("t"), HTTPClient: http.DefaultClient}
	inst := newInstances(client)
	node := &corev1.Node{Spec: corev1.NodeSpec{ProviderID: "hypervisor:///uuid-x"}}
	sd, err := inst.InstanceShutdown(context.TODO(), node)
	if err != nil {
		t.Fatal(err)
	}
	if !sd {
		t.Errorf("expected shutdown=true")
	}
}
