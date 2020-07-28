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

package contour

import (
	"testing"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/proto"
	"github.com/projectcontour/contour/internal/assert"
	"github.com/projectcontour/contour/internal/envoy"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	"k8s.io/utils/pointer"
)

func TestEndpointSliceTranslatorContents(t *testing.T) {
	tests := map[string]struct {
		contents map[string]map[string]*v2.ClusterLoadAssignment
		want     []proto.Message
	}{
		"empty": {
			contents: nil,
			want:     nil,
		},
		"simple": {
			contents: slicedClusterLoadAssignments(
				slicedClusterLoadAssignment(
					"httpbin-org-a1b2c",
					envoy.ClusterLoadAssignment("default/httpbin-org",
						envoy.SocketAddress("10.10.10.10", 80),
					),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.10.10", 80),
				),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var et EndpointSliceTranslator
			et.entries = tc.contents
			got := et.Contents()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEndpointSliceCacheQuery(t *testing.T) {
	tests := map[string]struct {
		contents map[string]map[string]*v2.ClusterLoadAssignment
		query    []string
		want     []proto.Message
	}{
		"exact match": {
			contents: slicedClusterLoadAssignments(
				slicedClusterLoadAssignment(
					"httpbin-org-a1b2c",
					envoy.ClusterLoadAssignment("default/httpbin-org",
						envoy.SocketAddress("10.10.10.10", 80),
					),
				),
			),
			query: []string{"default/httpbin-org"},
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.10.10", 80),
				),
			},
		},
		"partial match": {
			contents: slicedClusterLoadAssignments(
				slicedClusterLoadAssignment(
					"httpbin-org-a1b2c",
					envoy.ClusterLoadAssignment("default/httpbin-org",
						envoy.SocketAddress("10.10.10.10", 80),
					),
				),
			),
			query: []string{"default/kuard/8080", "default/httpbin-org"},
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.10.10", 80),
				),
				envoy.ClusterLoadAssignment("default/kuard/8080"),
			},
		},
		"no match": {
			contents: slicedClusterLoadAssignments(
				slicedClusterLoadAssignment(
					"httpbin-org-a1b2c",
					envoy.ClusterLoadAssignment("default/httpbin-org",
						envoy.SocketAddress("10.10.10.10", 80),
					),
				),
			),
			query: []string{"default/kuard/8080"},
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/kuard/8080"),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var et EndpointSliceTranslator
			et.entries = tc.contents
			got := et.Query(tc.query)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEndpointSliceTranslatorAddEndpointSlice(t *testing.T) {
	tests := map[string]struct {
		setup func(*EndpointSliceTranslator)
		eps   *discovery.EndpointSlice
		want  []proto.Message
	}{
		"simple": {
			eps: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/simple", envoy.SocketAddress("192.168.183.24", 8080)),
			},
		},
		"multiple endpoints": {
			eps: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("50.17.192.147"),
					epsliceEndpoint("50.17.206.192"),
					epsliceEndpoint("50.19.99.160"),
					epsliceEndpoint("23.23.247.89"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("23.23.247.89", 80), // addresses should be sorted
					envoy.SocketAddress("50.17.192.147", 80),
					envoy.SocketAddress("50.17.206.192", 80),
					envoy.SocketAddress("50.19.99.160", 80),
				),
			},
		},
		"multiple addresses": {
			eps: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint(
						"50.17.192.147",
						"50.17.206.192",
					),
					epsliceEndpoint(
						"50.19.99.160",
						"23.23.247.89",
					),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("23.23.247.89", 80), // addresses should be sorted
					envoy.SocketAddress("50.17.192.147", 80),
					envoy.SocketAddress("50.17.206.192", 80),
					envoy.SocketAddress("50.19.99.160", 80),
				),
			},
		},
		"multiple ports": {
			eps: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.1.1"),
				),
				epslicePorts(
					epslicePort("b", 309),
					epslicePort("a", 8675),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org/a", // cluster names should be sorted
					envoy.SocketAddress("10.10.1.1", 8675),
				),
				envoy.ClusterLoadAssignment("default/httpbin-org/b",
					envoy.SocketAddress("10.10.1.1", 309),
				),
			},
		},
		"cartesian product": {
			eps: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.2.2"),
					epsliceEndpoint("10.10.1.1"),
				),
				epslicePorts(
					epslicePort("b", 309),
					epslicePort("a", 8675),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org/a",
					envoy.SocketAddress("10.10.1.1", 8675), // addresses should be sorted
					envoy.SocketAddress("10.10.2.2", 8675),
				),
				envoy.ClusterLoadAssignment("default/httpbin-org/b",
					envoy.SocketAddress("10.10.1.1", 309),
					envoy.SocketAddress("10.10.2.2", 309),
				),
			},
		},
		"not ready": {
			eps: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.1.1"),
					epsliceEndpointNotReady("10.10.2.2"),
				),
				epslicePorts(
					epslicePort("", 8675),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.1.1", 8675),
				),
			},
		},
		"multiple slices": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "httpbin-org-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("10.10.2.2"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
			},
			eps: endpointSlice("default", "httpbin-org-b2c3d",
				epsliceEndpoints(
					epsliceEndpoint("10.10.1.1"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.1.1", 80), // addresses should be sorted
					envoy.SocketAddress("10.10.2.2", 80),
				),
			},
		},
		"multiple slices different ports": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "httpbin-org-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("10.10.2.2"),
					),
					epslicePorts(
						epslicePort("b", 309),
					),
				))
			},
			eps: endpointSlice("default", "httpbin-org-b2c3d",
				epsliceEndpoints(
					epsliceEndpoint("10.10.1.1"),
				),
				epslicePorts(
					epslicePort("a", 8675),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org/a", // cluster names should be sorted
					envoy.SocketAddress("10.10.1.1", 8675),
				),
				envoy.ClusterLoadAssignment("default/httpbin-org/b",
					envoy.SocketAddress("10.10.2.2", 309),
				),
			},
		},
	}

	log := testLogger(t)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			et := &EndpointSliceTranslator{
				FieldLogger: log,
			}
			if tc.setup != nil {
				tc.setup(et)
			}
			et.OnAdd(tc.eps)
			got := et.Contents()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEndpointSliceTranslatorRemoveEndpointSlice(t *testing.T) {
	tests := map[string]struct {
		setup func(*EndpointSliceTranslator)
		eps   *discovery.EndpointSlice
		want  []proto.Message
	}{
		"remove existing": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "simple-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("192.168.183.24"),
					),
					epslicePorts(
						epslicePort("", 8080),
					),
				))
			},
			eps: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: nil,
		},
		"remove different": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "simple-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("192.168.183.24"),
					),
					epslicePorts(
						epslicePort("", 8080),
					),
				))
			},
			eps: endpointSlice("default", "different-00000",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/simple", envoy.SocketAddress("192.168.183.24", 8080)),
			},
		},
		"remove non existent": {
			eps: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: nil,
		},
		"remove long name": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice(
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
				))
			},
			eps: endpointSlice(
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
			),
			want: nil,
		},
		"multiple slices": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "httpbin-org-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("10.10.2.2"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
				et.OnAdd(endpointSlice("default", "httpbin-org-b2c3d",
					epsliceEndpoints(
						epsliceEndpoint("10.10.1.1"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
			},
			eps: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.2.2"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.1.1", 80),
				),
			},
		},
	}

	log := testLogger(t)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			et := &EndpointSliceTranslator{
				FieldLogger: log,
			}
			if tc.setup != nil {
				tc.setup(et)
			}
			et.OnDelete(tc.eps)
			got := et.Contents()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEndpointSliceTranslatorRecomputeClusterLoadAssignment(t *testing.T) {
	tests := map[string]struct {
		setup        func(*EndpointSliceTranslator)
		oldep, newep *discovery.EndpointSlice
		want         []proto.Message
	}{
		"simple": {
			newep: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/simple", envoy.SocketAddress("192.168.183.24", 8080)),
			},
		},
		"multiple addresses": {
			newep: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("50.17.192.147"),
					epsliceEndpoint("23.23.247.89"),
					epsliceEndpoint("50.17.206.192"),
					epsliceEndpoint("50.19.99.160"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("23.23.247.89", 80),
					envoy.SocketAddress("50.17.192.147", 80),
					envoy.SocketAddress("50.17.206.192", 80),
					envoy.SocketAddress("50.19.99.160", 80),
				),
			},
		},
		"named container port": {
			newep: endpointSlice("default", "secure-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("https", 8443),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/secure/https", envoy.SocketAddress("192.168.183.24", 8443)),
			},
		},
		"remove existing": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "simple-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("192.168.183.24"),
					),
					epslicePorts(
						epslicePort("", 8080),
					),
				))
			},
			oldep: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: nil,
		},
		"update existing": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "simple-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("192.168.183.24"),
					),
					epslicePorts(
						epslicePort("", 8080),
					),
				))
			},
			oldep: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.24"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			newep: endpointSlice("default", "simple-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("192.168.183.25"),
				),
				epslicePorts(
					epslicePort("", 8080),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/simple", envoy.SocketAddress("192.168.183.25", 8080)),
			},
		},
		"multiple slices add": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "httpbin-org-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("10.10.2.2"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
			},
			newep: endpointSlice("default", "httpbin-org-b2c3d",
				epsliceEndpoints(
					epsliceEndpoint("10.10.1.1"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.1.1", 80), // addresses should be sorted
					envoy.SocketAddress("10.10.2.2", 80),
				),
			},
		},
		"multiple slices remove": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "httpbin-org-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("10.10.2.2"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
				et.OnAdd(endpointSlice("default", "httpbin-org-b2c3d",
					epsliceEndpoints(
						epsliceEndpoint("10.10.1.1"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
			},
			oldep: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.2.2"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.1.1", 80),
				),
			},
		},
		"multiple slices update": {
			setup: func(et *EndpointSliceTranslator) {
				et.OnAdd(endpointSlice("default", "httpbin-org-a1b2c",
					epsliceEndpoints(
						epsliceEndpoint("10.10.2.2"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
				et.OnAdd(endpointSlice("default", "httpbin-org-b2c3d",
					epsliceEndpoints(
						epsliceEndpoint("10.10.1.1"),
					),
					epslicePorts(
						epslicePort("", 80),
					),
				))
			},
			oldep: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.2.2"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			newep: endpointSlice("default", "httpbin-org-a1b2c",
				epsliceEndpoints(
					epsliceEndpoint("10.10.3.3"),
				),
				epslicePorts(
					epslicePort("", 80),
				),
			),
			want: []proto.Message{
				envoy.ClusterLoadAssignment("default/httpbin-org",
					envoy.SocketAddress("10.10.1.1", 80), // addresses should be sorted
					envoy.SocketAddress("10.10.3.3", 80),
				),
			},
		},
	}

	log := testLogger(t)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			et := &EndpointSliceTranslator{
				FieldLogger: log,
			}
			if tc.setup != nil {
				tc.setup(et)
			}
			et.recomputeClusterLoadAssignment(tc.oldep, tc.newep)
			got := et.Contents()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEndpointSliceTranslatorScaleToZeroEndpoints(t *testing.T) {
	var et EndpointSliceTranslator
	e1 := endpointSlice("default", "simple-a1b2c",
		epsliceEndpoints(
			epsliceEndpoint("192.168.183.24"),
		),
		epslicePorts(
			epslicePort("", 8080),
		),
	)
	et.OnAdd(e1)

	// Assert endpoint was added
	want := []proto.Message{
		envoy.ClusterLoadAssignment("default/simple", envoy.SocketAddress("192.168.183.24", 8080)),
	}
	got := et.Contents()

	assert.Equal(t, want, got)

	// e2 is the same as e1, but without any endpoints or ports
	e2 := endpointSlice("default", "simple-a1b2c", nil, nil)
	et.OnUpdate(e1, e2)

	// Assert endpoints are removed
	want = nil
	got = et.Contents()

	assert.Equal(t, want, got)
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
			Ready: pointer.BoolPtr(true),
		},
	}
}

func epsliceEndpointNotReady(ips ...string) discovery.Endpoint {
	ep := epsliceEndpoint(ips...)
	ep.Conditions.Ready = pointer.BoolPtr(false)
	return ep
}

func slicedClusterLoadAssignments(claSlices ...map[string]*v2.ClusterLoadAssignment) map[string]map[string]*v2.ClusterLoadAssignment {
	m := make(map[string]map[string]*v2.ClusterLoadAssignment)
	for _, claSlice := range claSlices {
		for sliceName, cla := range claSlice {
			clusterName := cla.ClusterName
			if m[clusterName] == nil {
				m[clusterName] = make(map[string]*v2.ClusterLoadAssignment)
			}
			m[clusterName][sliceName] = cla
		}
	}
	return m
}

func slicedClusterLoadAssignment(sliceName string, cla *v2.ClusterLoadAssignment) map[string]*v2.ClusterLoadAssignment {
	return map[string]*v2.ClusterLoadAssignment{
		sliceName: cla,
	}
}
