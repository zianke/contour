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
	"sort"
	"sync"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v2"
	"github.com/golang/protobuf/proto"
	"github.com/projectcontour/contour/internal/envoy"
	"github.com/projectcontour/contour/internal/protobuf"
	"github.com/projectcontour/contour/internal/sorter"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8scache "k8s.io/client-go/tools/cache"
	"k8s.io/utils/pointer"
)

// A EndpointSliceTranslator translates Kubernetes EndpointSlice objects into Envoy
// ClusterLoadAssignment objects.
type EndpointSliceTranslator struct {
	logrus.FieldLogger
	slicedClusterLoadAssignmentCache
}

func (e *EndpointSliceTranslator) OnAdd(obj interface{}) {
	switch obj := obj.(type) {
	case *discovery.EndpointSlice:
		e.addEndpointSlice(obj)
	default:
		e.Errorf("OnAdd unexpected type %T: %#v", obj, obj)
	}
}

func (e *EndpointSliceTranslator) OnUpdate(oldObj, newObj interface{}) {
	switch newObj := newObj.(type) {
	case *discovery.EndpointSlice:
		oldObj, ok := oldObj.(*discovery.EndpointSlice)
		if !ok {
			e.Errorf("OnUpdate endpoints %#v received invalid oldObj %T; %#v", newObj, oldObj, oldObj)
			return
		}
		e.updateEndpointSlice(oldObj, newObj)
	default:
		e.Errorf("OnUpdate unexpected type %T: %#v", newObj, newObj)
	}
}

func (e *EndpointSliceTranslator) OnDelete(obj interface{}) {
	switch obj := obj.(type) {
	case *discovery.EndpointSlice:
		e.removeEndpointSlice(obj)
	case k8scache.DeletedFinalStateUnknown:
		e.OnDelete(obj.Obj) // recurse into ourselves with the tombstoned value
	default:
		e.Errorf("OnDelete unexpected type %T: %#v", obj, obj)
	}
}

func (e *EndpointSliceTranslator) Contents() []proto.Message {
	values := e.slicedClusterLoadAssignmentCache.Contents()
	sort.Stable(sorter.For(values))
	return protobuf.AsMessages(values)
}

func (e *EndpointSliceTranslator) Query(names []string) []proto.Message {
	e.slicedClusterLoadAssignmentCache.mu.Lock()
	defer e.slicedClusterLoadAssignmentCache.mu.Unlock()
	values := make([]*v2.ClusterLoadAssignment, 0, len(names))
	for _, n := range names {
		var v *v2.ClusterLoadAssignment
		slices, ok := e.entries[n]
		if !ok {
			v = &v2.ClusterLoadAssignment{
				ClusterName: n,
			}
		} else {
			v = mergeSlicedCLA(n, slices)
		}
		values = append(values, v)
	}

	sort.Stable(sorter.For(values))
	return protobuf.AsMessages(values)
}

func (*EndpointSliceTranslator) TypeURL() string { return resource.EndpointType }

func (e *EndpointSliceTranslator) addEndpointSlice(eps *discovery.EndpointSlice) {
	e.recomputeClusterLoadAssignment(nil, eps)
}

func (e *EndpointSliceTranslator) updateEndpointSlice(oldeps, neweps *discovery.EndpointSlice) {
	if len(neweps.Endpoints) == 0 && len(oldeps.Endpoints) == 0 {
		// if there are no endpoints in this object, and the old
		// object also had zero endpoints, ignore this update
		// to avoid sending a noop notification to watchers.
		return
	}
	e.recomputeClusterLoadAssignment(oldeps, neweps)
}

func (e *EndpointSliceTranslator) removeEndpointSlice(eps *discovery.EndpointSlice) {
	e.recomputeClusterLoadAssignment(eps, nil)
}

// recomputeClusterLoadAssignment recomputes the EDS cache taking into account old and new EndpointSlices.
func (e *EndpointSliceTranslator) recomputeClusterLoadAssignment(oldeps, neweps *discovery.EndpointSlice) {
	// skip computation if old and new EndpointSlices are equal (thus also handling nil)
	if oldeps == neweps {
		return
	}

	if oldeps == nil {
		oldeps = &discovery.EndpointSlice{
			ObjectMeta: neweps.ObjectMeta,
		}
	}

	if neweps == nil {
		neweps = &discovery.EndpointSlice{
			ObjectMeta: oldeps.ObjectMeta,
		}
	}

	seen := make(map[string]bool)
	// add or update endpoints
	for _, p := range neweps.Ports {
		if p.Protocol != nil && *p.Protocol != v1.ProtocolTCP {
			// skip non TCP ports
			continue
		}

		addresses := make([]string, 0, len(neweps.Endpoints))
		for _, s := range neweps.Endpoints {
			if len(s.Addresses) < 1 {
				// skip endpoint without addresses.
				continue
			}
			if s.Conditions.Ready == nil || !*s.Conditions.Ready {
				// skip endpoint without ready condition
				continue
			}

			addresses = append(addresses, s.Addresses...) // shallow copy
		}

		sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })

		lbendpoints := make([]*envoy_api_v2_endpoint.LbEndpoint, 0, len(addresses))
		for _, a := range addresses {
			addr := envoy.SocketAddress(a, int(*p.Port))
			lbendpoints = append(lbendpoints, envoy.LBEndpoint(addr))
		}

		servicemeta := metav1.ObjectMeta{
			Namespace: neweps.Namespace,
			Name:      neweps.Labels[discovery.LabelServiceName],
		}
		portname := pointer.StringPtrDerefOr(p.Name, "")
		cla := &v2.ClusterLoadAssignment{
			ClusterName: servicename(servicemeta, portname),
			Endpoints: []*envoy_api_v2_endpoint.LocalityLbEndpoints{{
				LbEndpoints: lbendpoints,
			}},
		}
		seen[cla.ClusterName] = true
		e.Add(cla, neweps.Name)
	}

	// iterate over the ports in the old spec, remove any were not seen.
	for _, s := range oldeps.Endpoints {
		if len(s.Addresses) == 0 {
			continue
		}
		for _, p := range oldeps.Ports {
			servicemeta := metav1.ObjectMeta{
				Namespace: oldeps.Namespace,
				Name:      oldeps.Labels[discovery.LabelServiceName],
			}
			portname := pointer.StringPtrDerefOr(p.Name, "")
			clustername := servicename(servicemeta, portname)
			if _, ok := seen[clustername]; !ok {
				// port is no longer present, remove the corresponding EndpointSlice.
				e.Remove(clustername, oldeps.Name)
			}
		}
	}
}

type slicedClusterLoadAssignmentCache struct {
	mu sync.Mutex
	// entries[clusterName][endpointSliceName] represents a cluster's endpoints within an EndpointSlice.
	entries map[string]map[string]*v2.ClusterLoadAssignment
	Cond
}

// Add adds an entry to the cache. If a ClusterLoadAssignment with the same
// cluster name and EndpointSlices name exists, it is replaced.
func (c *slicedClusterLoadAssignmentCache) Add(a *v2.ClusterLoadAssignment, sliceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]map[string]*v2.ClusterLoadAssignment)
	}
	if c.entries[a.ClusterName] == nil {
		c.entries[a.ClusterName] = make(map[string]*v2.ClusterLoadAssignment)
	}
	c.entries[a.ClusterName][sliceName] = a
	c.Notify(a.ClusterName)
}

// Remove removes the named entry from the cache. If the entry
// is not present in the cache, the operation is a no-op.
func (c *slicedClusterLoadAssignmentCache) Remove(clusterName string, sliceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries[clusterName], sliceName)
	if len(c.entries[clusterName]) == 0 {
		delete(c.entries, clusterName)
	}
	c.Notify(clusterName)
}

// Contents returns the contents of the cache by merging all the
// ClusterLoadAssignments of the same cluster.
func (c *slicedClusterLoadAssignmentCache) Contents() []*v2.ClusterLoadAssignment {
	c.mu.Lock()
	defer c.mu.Unlock()
	values := make([]*v2.ClusterLoadAssignment, 0, len(c.entries))
	for name, slices := range c.entries {
		cla := mergeSlicedCLA(name, slices)
		values = append(values, cla)
	}
	return values
}

// mergeSlicedCLA merges the ClusterLoadAssignments of the same cluster.
func mergeSlicedCLA(name string, slices map[string]*v2.ClusterLoadAssignment) *v2.ClusterLoadAssignment {
	lbendpoints := make([]*envoy_api_v2_endpoint.LbEndpoint, 0, len(slices))
	for _, slice := range slices {
		for _, ep := range slice.Endpoints {
			lbendpoints = append(lbendpoints, ep.LbEndpoints...)
		}
	}
	sort.Stable(sorter.For(lbendpoints))
	cla := &v2.ClusterLoadAssignment{
		ClusterName: name,
		Endpoints: []*envoy_api_v2_endpoint.LocalityLbEndpoints{{
			LbEndpoints: lbendpoints,
		}},
	}
	return cla
}
