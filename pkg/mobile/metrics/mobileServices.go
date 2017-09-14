package metrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/feedhenry/mcp-standalone/pkg/mobile"
)

// GathererScheduler schedules metrics Gathering Jobs
type GathererScheduler struct {
	ticker    *time.Ticker
	cancel    chan struct{}
	jobs      map[string]Gatherer
	logger    *logrus.Logger
	metrics   *metricsMap
	waitGroup *sync.WaitGroup
}

//might be able to make internal
type metric struct {
	Type   string
	XValue string
	YValue int
}

// Gatherer is something that knows how to Gather metrics
type Gatherer func() ([]*metric, error)

// NewGathererScheduler creates a default GathererScheduler
func NewGathererScheduler(ticker *time.Ticker, cancel chan struct{}, logger *logrus.Logger) *GathererScheduler {
	return &GathererScheduler{
		ticker:    ticker,
		cancel:    cancel,
		jobs:      map[string]Gatherer{},
		logger:    logger,
		metrics:   internalMetrics,
		waitGroup: &sync.WaitGroup{},
	}
}

type metricsMap struct {
	data map[string][]*mobile.GatheredMetric
	*sync.RWMutex
}

var internalMetrics = &metricsMap{
	RWMutex: &sync.RWMutex{},
	data:    map[string][]*mobile.GatheredMetric{},
}

func (mm *metricsMap) add(name string, m *metric) {
	fmt.Println("addming ", m, "to ", name)
	mm.Lock()
	defer mm.Unlock()
	gathered := mm.data[name]
	if len(gathered) == 0 {
		gathered = []*mobile.GatheredMetric{{
			Type: m.Type,
			X:    []string{},
			Y:    map[string][]int{},
		}}
		mm.data[name] = gathered
	}
	for i := range gathered {
		gm := gathered[i]
		if gm.Type == m.Type {
			gm.X = append(gm.X, m.XValue)
			gm.Y[gm.Type] = append(gm.Y[gm.Type], m.YValue)
		}
		gathered[i] = gm
	}
}

func (mm *metricsMap) read(name string) []*mobile.GatheredMetric {
	mm.RLock()
	defer mm.RUnlock()
	return mm.data[name]
}

// Add allows new Gatherers to be added
func (gs *GathererScheduler) Add(serviceName string, metricGatherer Gatherer) {
	gs.jobs[serviceName] = metricGatherer
}

// Run will start the jobs on a schedule
func (gs *GathererScheduler) Run() {
	for {
		select {
		case <-gs.ticker.C:
			gs.execute()
			//run gather jobs
		case <-gs.cancel:
			gs.logger.Info("stopping metrics gatherers")
			gs.ticker.Stop()
			gs.logger.Info("ticker stopped")
			return
		}
	}
}

func (gs *GathererScheduler) execute() {
	//wait for the previous group to be done. If all completed will continue on
	gs.logger.Debug("executing gatherers after previos set done")
	gs.waitGroup.Wait()
	gs.logger.Debug("executing gatherers previos complete")
	for s, g := range gs.jobs {
		go func(service string, gather Gatherer) {
			gs.waitGroup.Add(1)
			defer gs.waitGroup.Done()
			ms, err := gather()
			if err != nil {
				gs.logger.Error("failed to gather metrics for service ", service, err)
			}
			fmt.Println("gathered metrics for service ", service, ms)
			for _, m := range ms {
				gs.metrics.add(service, m)
			}
		}(s, g)
	}
}

type MetricsService struct{}

// Get will return the gathered metrics for a service
func (ms *MetricsService) GetAll(serviceName string) []*mobile.GatheredMetric {
	internalMetrics.RLock()
	defer internalMetrics.RUnlock()
	return internalMetrics.data[serviceName]
}

func (ms *MetricsService) GetOne(serviceName, metric string) *mobile.GatheredMetric {
	internalMetrics.RLock()
	defer internalMetrics.RUnlock()
	all := internalMetrics.data[serviceName]
	if len(all) == 0 {
		return nil
	}
	for _, m := range all {
		if m.Type == metric {
			return m
		}
	}
	return nil
}