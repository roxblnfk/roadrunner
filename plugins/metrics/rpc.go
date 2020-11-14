package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spiral/errors"
	"github.com/spiral/roadrunner/v2/interfaces/log"
)

type rpcServer struct {
	svc *Plugin
	log log.Logger
}

// Metric represent single metric produced by the application.
type Metric struct {
	// Collector name.
	Name string

	// Collector value.
	Value float64

	// Labels associated with metric. Only for vector metrics. Must be provided in a form of label values.
	Labels []string
}

// Add new metric to the designated collector.
func (rpc *rpcServer) Add(m *Metric, ok *bool) error {
	const op = errors.Op("Add metric")
	rpc.log.Info("Adding metric", "name", m.Name, "value", m.Value, "labels", m.Labels)
	c, exist := rpc.svc.collectors.Load(m.Name)
	if !exist {
		rpc.log.Error("undefined collector", "collector", m.Name)
		return errors.E(op, errors.Errorf("undefined collector %s, try first Declare the desired collector", m.Name))
	}

	switch c := c.(type) {
	case prometheus.Gauge:
		c.Add(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		c.WithLabelValues(m.Labels...).Add(m.Value)

	case prometheus.Counter:
		c.Add(m.Value)

	case *prometheus.CounterVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		c.WithLabelValues(m.Labels...).Add(m.Value)

	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method `Add`", m.Name))
	}

	// RPC, set ok to true as return value. Need by rpc.Call reply argument
	*ok = true
	rpc.log.Info("new metric successfully added")
	return nil
}

// Sub subtract the value from the specific metric (gauge only).
func (rpc *rpcServer) Sub(m *Metric, ok *bool) error {
	const op = errors.Op("Sub metric")
	c, exist := rpc.svc.collectors.Load(m.Name)
	if !exist {
		return errors.E(op, errors.Errorf("undefined collector `%s`", m.Name))
	}
	if c == nil {
		// can it be nil ??? I guess can't
		return errors.E(op, errors.Errorf("undefined collector `%s`", m.Name))
	}

	switch c := c.(type) {
	case prometheus.Gauge:
		c.Sub(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		c.WithLabelValues(m.Labels...).Sub(m.Value)
	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method `Sub`", m.Name))
	}

	// RPC, set ok to true as return value. Need by rpc.Call reply argument
	*ok = true
	return nil
}

// Observe the value (histogram and summary only).
func (rpc *rpcServer) Observe(m *Metric, ok *bool) error {
	const op = errors.Op("Observe metrics")
	c, exist := rpc.svc.collectors.Load(m.Name)
	if !exist {
		return errors.E(op, errors.Errorf("undefined collector `%s`", m.Name))
	}
	if c == nil {
		return errors.E(op, errors.Errorf("undefined collector `%s`", m.Name))
	}

	switch c := c.(type) {
	case *prometheus.SummaryVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		c.WithLabelValues(m.Labels...).Observe(m.Value)

	case prometheus.Histogram:
		c.Observe(m.Value)

	case *prometheus.HistogramVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		c.WithLabelValues(m.Labels...).Observe(m.Value)
	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method `Observe`", m.Name))
	}

	// RPC, set ok to true as return value. Need by rpc.Call reply argument
	*ok = true
	return nil
}

// Declare is used to register new collector in prometheus
// THE TYPES ARE:
// 	NamedCollector -> Collector with the name
// 	bool -> RPC reply value
// RETURNS:
// 	error
func (rpc *rpcServer) Declare(nc *NamedCollector, ok *bool) error {
	const op = errors.Op("Declare metric")
	_, exist := rpc.svc.collectors.Load(nc.Name)
	if exist {
		return errors.E(op, errors.Errorf("tried to register existing collector with the name `%s`", nc.Name))
	}

	var collector prometheus.Collector
	switch nc.Type {
	case Histogram:
		opts := prometheus.HistogramOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
			Buckets:   nc.Buckets,
		}

		if len(nc.Labels) != 0 {
			collector = prometheus.NewHistogramVec(opts, nc.Labels)
		} else {
			collector = prometheus.NewHistogram(opts)
		}
	case Gauge:
		opts := prometheus.GaugeOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
		}

		if len(nc.Labels) != 0 {
			collector = prometheus.NewGaugeVec(opts, nc.Labels)
		} else {
			collector = prometheus.NewGauge(opts)
		}
	case Counter:
		opts := prometheus.CounterOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
		}

		if len(nc.Labels) != 0 {
			collector = prometheus.NewCounterVec(opts, nc.Labels)
		} else {
			collector = prometheus.NewCounter(opts)
		}
	case Summary:
		opts := prometheus.SummaryOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
		}

		if len(nc.Labels) != 0 {
			collector = prometheus.NewSummaryVec(opts, nc.Labels)
		} else {
			collector = prometheus.NewSummary(opts)
		}

	default:
		return errors.E(op, errors.Errorf("unknown collector type %s", nc.Type))
	}

	// add collector to sync.Map
	rpc.svc.collectors.Store(nc.Name, collector)
	// that method might panic, we handle it by recover
	err := rpc.svc.Register(collector)
	if err != nil {
		*ok = false
		return errors.E(op, err)
	}

	*ok = true
	return nil
}

// Set the metric value (only for gaude).
func (rpc *rpcServer) Set(m *Metric, ok *bool) (err error) {
	const op = errors.Op("Set metric")
	defer func() {
		if r, fail := recover().(error); fail {
			err = r
		}
	}()

	c, exist := rpc.svc.collectors.Load(m.Name)
	if !exist {
		return errors.E(op, errors.Errorf("undefined collector `%s`", m.Name))
	}
	if c == nil {
		return errors.E(op, errors.Errorf("undefined collector `%s`", m.Name))
	}

	switch c := c.(type) {
	case prometheus.Gauge:
		c.Set(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		c.WithLabelValues(m.Labels...).Set(m.Value)

	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method `Set`", m.Name))
	}

	// RPC, set ok to true as return value. Need by rpc.Call reply argument
	*ok = true
	return nil
}
