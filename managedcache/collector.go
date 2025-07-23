package managedcache

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Collector = (*collector[client.Object])(nil)

const (
	ownerLabel = "owner"
	gvkLabel   = "gvk"
)

type Collector prometheus.Collector

func NewCollector[T RefType](manager ObjectBoundAccessManager[T], metricsPrefix string) Collector {
	informersDesc := prometheus.NewDesc(
		metricsPrefix+"_managed_cache_informers",
		"Number of active informers per owner running for the managed cache.",
		[]string{ownerLabel}, nil)
	objectsDesc := prometheus.NewDesc(
		metricsPrefix+"_managed_cache_objects",
		"Number of objects per GVK and owner in the managed cache.",
		[]string{ownerLabel, gvkLabel}, nil)

	return &collector[T]{
		manager,
		informersDesc,
		objectsDesc,
	}
}

type collector[T RefType] struct {
	manager       ObjectBoundAccessManager[T]
	informersDesc *prometheus.Desc
	objectsDesc   *prometheus.Desc
}

func (c collector[T]) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}

func (c collector[T]) Collect(ch chan<- prometheus.Metric) {
	c.manager.readAccessors(func(owner types.UID, accessor Accessor) {
		gvks := accessor.GetGVKs()

		// Number of GVKs per owner
		ch <- prometheus.MustNewConstMetric(
			c.informersDesc,
			prometheus.GaugeValue,
			float64(len(gvks)),
			string(owner),
		)

		// TODO(reviewer): this way the two metrics may not match if informers
		// change in between the calls. Do we care? (IMO, it should be fine)

		for gvk, objects := range accessor.getObjectsPerInformer(context.Background()) {
			// Number of objects per GVK per owner
			ch <- prometheus.MustNewConstMetric(
				c.objectsDesc,
				prometheus.GaugeValue,
				float64(objects),
				string(owner), gvk.String(),
			)
		}
	})
}
