package builder

import "sync"

// Pool 管理延迟边
type Pool struct {
	Name       string
	Depth      int
	currentUse int
	delayed    []*Edge
	mu         sync.Mutex
}

func NewPool(name string, depth int) *Pool {
	return &Pool{Name: name, Depth: depth}
}

func (p *Pool) ShouldDelayEdge() bool {
	return p.Depth != 0
}

func (p *Pool) EdgeScheduled(edge *Edge) {
	if p.Depth == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentUse += edge.Weight()
}

func (p *Pool) EdgeFinished(edge *Edge) {
	if p.Depth == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentUse -= edge.Weight()
}

func (p *Pool) DelayEdge(edge *Edge) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delayed = append(p.delayed, edge)
}

func (p *Pool) RetrieveReadyEdges(queue *EdgePriorityQueue) {
	if p.Depth == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var remaining []*Edge
	for _, e := range p.delayed {
		if p.currentUse+e.Weight() <= p.Depth {
			p.currentUse += e.Weight()
			queue.Push(e)
		} else {
			remaining = append(remaining, e)
		}
	}
	p.delayed = remaining
}
