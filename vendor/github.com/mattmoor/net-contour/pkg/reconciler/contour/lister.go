/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package contour

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/mattmoor/net-contour/pkg/reconciler/contour/config"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/ingress"
	"knative.dev/serving/pkg/network/status"
)

type lister struct {
	ServiceLister   corev1listers.ServiceLister
	EndpointsLister corev1listers.EndpointsLister
}

var _ status.ProbeTargetLister = (*lister)(nil)

// ListProbeTargets implements status.ProbeTargetLister
func (l *lister) ListProbeTargets(ctx context.Context, ing *v1alpha1.Ingress) ([]status.ProbeTarget, error) {
	var results []status.ProbeTarget

	visibilityKeys := config.FromContext(ctx).Contour.VisibilityKeys
	for key, hosts := range ingress.HostsPerVisibility(ing, visibilityKeys) {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse key: %w", err)
		}

		service, err := l.ServiceLister.Services(namespace).Get(name)
		if err != nil {
			return nil, fmt.Errorf("failed to get Service: %w", err)
		}

		endpoints, err := l.EndpointsLister.Endpoints(namespace).Get(name)
		if err != nil {
			return nil, fmt.Errorf("failed to get Endpoints: %w", err)
		}

		urls := make([]*url.URL, 0, hosts.Len())
		for _, host := range hosts.UnsortedList() {
			urls = append(urls, &url.URL{
				Scheme: "http",
				Host:   host,
			})
		}

		// TODO(mattmoor): Perhaps key off of whether HTTP is enabled?
		portName, err := network.NameForPortNumber(service, 80)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup port 80 in %s/%s: %v", namespace, name, err)
		}
		for _, sub := range endpoints.Subsets {
			portNumber, err := network.PortNumberForName(sub, portName)
			if err != nil {
				return nil, fmt.Errorf("failed to lookup port name %q in endpoints subset for %s/%s: %v",
					portName, namespace, name, err)
			}

			pt := status.ProbeTarget{
				PodIPs:  sets.NewString(),
				Port:    "80",
				PodPort: strconv.Itoa(int(portNumber)),
				URLs:    urls,
			}
			for _, addr := range sub.Addresses {
				pt.PodIPs.Insert(addr.IP)
			}
			results = append(results, pt)
		}

	}

	return results, nil
}
