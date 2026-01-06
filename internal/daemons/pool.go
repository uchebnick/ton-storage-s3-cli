package daemons

import (
	"context"
	"sync"
)

type DaemonFunc func(ctx context.Context, workerID int, totalWorkers int)

type DaemonPool struct {
	daemonFunc	DaemonFunc
	workersCount	int
	wg		sync.WaitGroup
	ctx		context.Context
	cancel		context.CancelFunc
	mu		sync.RWMutex
}

func NewPool(ctx context.Context, numWorkers int, daemon DaemonFunc) *DaemonPool {
	ctx, cancel := context.WithCancel(ctx)

	return &DaemonPool{
		daemonFunc:	daemon,
		workersCount:	numWorkers,
		ctx:		ctx,
		cancel:		cancel,
	}
}

func (p *DaemonPool) Start() {
	p.mu.RLock()
	count := p.workersCount
	p.mu.RUnlock()

	for i := 0; i < count; i++ {
		p.wg.Add(1)
		workerID := i

		go func(id int) {
			defer p.wg.Done()
			p.daemonFunc(p.ctx, id, count)
		}(workerID)
	}
}

func (p *DaemonPool) Stop() {
	p.cancel()
	p.wg.Wait()
}

func (p *DaemonPool) GetWorkerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.workersCount
}
