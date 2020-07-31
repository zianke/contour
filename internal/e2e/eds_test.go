// Copyright Project Contour Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"strings"
	"testing"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/projectcontour/contour/internal/assert"
	"github.com/projectcontour/contour/internal/envoy"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilpointer "k8s.io/utils/pointer"
)

// test that adding and removing endpoints don't leave turds
// in the eds cache.
func TestAddRemoveEndpoints(t *testing.T) {
	rh, cc, done := setup(t)
	defer done()

	// e1 is a simple endpointslice for two hosts, and two ports
	// it has a long name to check that it's clustername is _not_
	// hashed.
	e1 := endpointSlice(
		"super-long-namespace-name-oh-boy",
		"what-a-descriptive-service-name-you-must-be-so-proud-a1b2c",
		epsliceEndpoints(
			epsliceEndpoint("172.16.0.2"),
			epsliceEndpoint("172.16.0.1"),
		),
		epslicePorts(
			epslicePort("https", 8443),
			epslicePort("http", 8080),
		),
	)

	rh.OnAdd(e1)

	// check that it's been translated correctly.
	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "2",
		Resources: resources(t,
			envoy.ClusterLoadAssignment(
				"super-long-namespace-name-oh-boy/what-a-descriptive-service-name-you-must-be-so-proud/http",
				envoy.SocketAddress("172.16.0.1", 8080), // endpoints and cluster names should be sorted
				envoy.SocketAddress("172.16.0.2", 8080),
			),
			envoy.ClusterLoadAssignment(
				"super-long-namespace-name-oh-boy/what-a-descriptive-service-name-you-must-be-so-proud/https",
				envoy.SocketAddress("172.16.0.1", 8443),
				envoy.SocketAddress("172.16.0.2", 8443),
			),
		),
		TypeUrl: endpointType,
		Nonce:   "2",
	}, streamEDS(t, cc))

	// remove e1 and check that the EDS cache is now empty.
	rh.OnDelete(e1)

	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "4",
		Resources:   resources(t),
		TypeUrl:     endpointType,
		Nonce:       "4",
	}, streamEDS(t, cc))
}

func TestAddEndpointComplicated(t *testing.T) {
	rh, cc, done := setup(t)
	defer done()

	e1 := endpointSlice("default", "kuard-a1b2c",
		epsliceEndpoints(
			epsliceEndpoint("10.48.1.78"),
			epsliceEndpointNotReady("10.48.1.77"),
		),
		epslicePorts(
			epslicePort("foo", 8080),
		),
	)

	e2 := endpointSlice("default", "kuard-b2c3d",
		epsliceEndpoints(
			epsliceEndpoint("10.48.1.78"),
			epsliceEndpoint("10.48.1.77"),
		),
		epslicePorts(
			epslicePort("admin", 9000),
		),
	)

	e3 := endpointSlice("default", "kuard-c3d4e",
		epsliceEndpoints(
			epsliceEndpoint("10.48.1.75"),
			epsliceEndpoint("10.48.1.76"),
		),
		epslicePorts(
			epslicePort("foo", 8080),
			epslicePort("admin", 9000),
		),
	)

	rh.OnAdd(e1)
	rh.OnAdd(e2)
	rh.OnAdd(e3)

	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "2",
		Resources: resources(t,
			envoy.ClusterLoadAssignment(
				"default/kuard/admin",
				envoy.SocketAddress("10.48.1.75", 9000), // endpoints and cluster names should be sorted
				envoy.SocketAddress("10.48.1.76", 9000),
				envoy.SocketAddress("10.48.1.77", 9000),
				envoy.SocketAddress("10.48.1.78", 9000),
			),
			envoy.ClusterLoadAssignment(
				"default/kuard/foo",
				envoy.SocketAddress("10.48.1.75", 8080),
				envoy.SocketAddress("10.48.1.76", 8080),
				envoy.SocketAddress("10.48.1.78", 8080),
			),
		),
		TypeUrl: endpointType,
		Nonce:   "2",
	}, streamEDS(t, cc))
}

func TestEndpointFilter(t *testing.T) {
	rh, cc, done := setup(t)
	defer done()

	e1 := endpointSlice("default", "kuard-a1b2c",
		epsliceEndpoints(
			epsliceEndpoint("10.48.1.78"),
			epsliceEndpointNotReady("10.48.1.77"),
		),
		epslicePorts(
			epslicePort("foo", 8080),
		),
	)

	e2 := endpointSlice("default", "kuard-b2c3d",
		epsliceEndpoints(
			epsliceEndpoint("10.48.1.78"),
			epsliceEndpoint("10.48.1.77"),
		),
		epslicePorts(
			epslicePort("admin", 9000),
		),
	)

	e3 := endpointSlice("default", "kuard-c3d4e",
		epsliceEndpoints(
			epsliceEndpoint("10.48.1.75"),
			epsliceEndpoint("10.48.1.76"),
		),
		epslicePorts(
			epslicePort("foo", 8080),
			epslicePort("admin", 9000),
		),
	)

	rh.OnAdd(e1)
	rh.OnAdd(e2)
	rh.OnAdd(e3)

	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "2",
		Resources: resources(t,
			envoy.ClusterLoadAssignment(
				"default/kuard/foo",
				envoy.SocketAddress("10.48.1.75", 8080), // endpoints and cluster names should be sorted
				envoy.SocketAddress("10.48.1.76", 8080),
				envoy.SocketAddress("10.48.1.78", 8080),
			),
		),
		TypeUrl: endpointType,
		Nonce:   "2",
	}, streamEDS(t, cc, "default/kuard/foo"))

	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "2",
		TypeUrl:     endpointType,
		Resources: resources(t,
			envoy.ClusterLoadAssignment("default/kuard/bar"),
		),
		Nonce: "2",
	}, streamEDS(t, cc, "default/kuard/bar"))

}

// issue 602, test that an update from N endpoints
// to zero endpoints is handled correctly.
func TestIssue602(t *testing.T) {
	rh, cc, done := setup(t)
	defer done()

	e1 := endpointSlice("default", "simple-a1b2c",
		epsliceEndpoints(
			epsliceEndpoint("192.168.183.24"),
		),
		epslicePorts(
			epslicePort("", 8080),
		),
	)
	rh.OnAdd(e1)

	// Assert endpointslice was added
	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "1",
		Resources: resources(t,
			envoy.ClusterLoadAssignment("default/simple", envoy.SocketAddress("192.168.183.24", 8080)),
		),
		TypeUrl: endpointType,
		Nonce:   "1",
	}, streamEDS(t, cc))

	// e2 is the same as e1, but without any endpoints or ports
	e2 := endpointSlice("default", "simple-a1b2c", nil, nil)
	rh.OnUpdate(e1, e2)

	assert.Equal(t, &v2.DiscoveryResponse{
		VersionInfo: "2",
		Resources:   resources(t),
		TypeUrl:     endpointType,
		Nonce:       "2",
	}, streamEDS(t, cc))
}

func streamEDS(t *testing.T, cc *grpc.ClientConn, rn ...string) *v2.DiscoveryResponse {
	t.Helper()
	rds := v2.NewEndpointDiscoveryServiceClient(cc)
	st, err := rds.StreamEndpoints(context.TODO())
	check(t, err)
	return stream(t, st, &v2.DiscoveryRequest{
		TypeUrl:       endpointType,
		ResourceNames: rn,
	})
}

func endpointSlice(ns, name string, endpoints []discovery.Endpoint, ports []discovery.EndpointPort) *discovery.EndpointSlice {
	return &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				discovery.LabelServiceName: name[:strings.LastIndex(name, "-")],
			},
		},
		AddressType: discovery.AddressTypeIPv4,
		Endpoints:   endpoints,
		Ports:       ports,
	}
}

func epslicePorts(eps ...discovery.EndpointPort) []discovery.EndpointPort {
	return eps
}

func epslicePort(name string, port int32) discovery.EndpointPort {
	protocol := v1.ProtocolTCP
	return discovery.EndpointPort{
		Name:     &name,
		Port:     &port,
		Protocol: &protocol,
	}
}

func epsliceEndpoints(eps ...discovery.Endpoint) []discovery.Endpoint {
	return eps
}

func epsliceEndpoint(ips ...string) discovery.Endpoint {
	return discovery.Endpoint{
		Addresses: ips,
		Conditions: discovery.EndpointConditions{
			Ready: utilpointer.BoolPtr(true),
		},
	}
}

func epsliceEndpointNotReady(ips ...string) discovery.Endpoint {
	ep := epsliceEndpoint(ips...)
	ep.Conditions.Ready = utilpointer.BoolPtr(false)
	return ep
}
