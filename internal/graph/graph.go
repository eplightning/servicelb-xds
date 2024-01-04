package graph

import (
	"context"
	"fmt"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/eplightning/servicelb-xds/internal"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sync"
	"time"
)

const (
	snapshotTimer = 3 * time.Second
)

type graphData struct {
	services map[types.NamespacedName]*ServiceData
}

type ServiceGraph struct {
	l logr.Logger

	config  *internal.Config
	data    *graphData
	version int64
	sc      cache.SnapshotCache
	mut     sync.Mutex

	closed   bool
	signalCh chan interface{}
}

func NewServiceGraph(config *internal.Config, logger logr.Logger) *ServiceGraph {
	return &ServiceGraph{
		config:   config,
		data:     newGraphData(),
		l:        logger,
		sc:       cache.NewSnapshotCache(false, constHash{}, nil),
		signalCh: make(chan interface{}, 1),
	}
}

func (g *ServiceGraph) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		g.closed = true
		close(g.signalCh)
	}()

	for {
		_, ok := <-g.signalCh
		if !ok {
			return nil
		}

		g.newSnapshot()

		time.Sleep(snapshotTimer)
	}
}

func (g *ServiceGraph) GetCache() cache.SnapshotCache {
	return g.sc
}

func (g *ServiceGraph) RemoveService(name types.NamespacedName) {
	g.mut.Lock()
	defer g.mut.Unlock()

	if _, ok := g.data.services[name]; ok {
		delete(g.data.services, name)
		g.notify()
	}
}

func (g *ServiceGraph) UpdateService(name types.NamespacedName, data *ServiceData) {
	g.mut.Lock()
	defer g.mut.Unlock()

	g.data.services[name] = data
	g.notify()
}

func (g *ServiceGraph) Conflicts(name types.NamespacedName, port ServicePort) bool {
	g.mut.Lock()
	defer g.mut.Unlock()

	for svc, data := range g.data.services {
		if name.Name == svc.Name && name.Namespace == svc.Namespace {
			continue
		}

		for svcPort, _ := range data.Ports {
			if svcPort.Port == port.Port && svcPort.Protocol == port.Protocol {
				return true
			}
		}
	}

	return false
}

func (g *ServiceGraph) notify() {
	if g.closed {
		return
	}

	select {
	case g.signalCh <- nil:
	default:
	}
}

func (g *ServiceGraph) newSnapshot() {
	g.mut.Lock()
	defer g.mut.Unlock()

	g.version++

	snapshot, err := cache.NewSnapshot(fmt.Sprintf("%v", g.version), buildSnapshotData(g.config, g.data))
	if err != nil {
		g.l.Error(err, "could not create snapshot")
		return
	}

	err = g.sc.SetSnapshot(context.Background(), "node", snapshot)
	if err != nil {
		g.l.Error(err, "could not set snapshot")
		return
	}
}

func newGraphData() *graphData {
	return &graphData{
		services: make(map[types.NamespacedName]*ServiceData),
	}
}
