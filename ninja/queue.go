package main

type EdgePriorityQueue struct {
	edges []*Edge
}

func (q *EdgePriorityQueue) Len() int { return len(q.edges) }
func (q *EdgePriorityQueue) Less(i, j int) bool {
	// 按 critical path weight 降序，再按 id_ 升序
	if q.edges[i].critical_path_weight_ != q.edges[j].critical_path_weight_ {
		return q.edges[i].critical_path_weight_ > q.edges[j].critical_path_weight_
	}
	return q.edges[i].id_ < q.edges[j].id_
}
func (q *EdgePriorityQueue) Swap(i, j int)      { q.edges[i], q.edges[j] = q.edges[j], q.edges[i] }
func (q *EdgePriorityQueue) Push(x interface{}) { q.edges = append(q.edges, x.(*Edge)) }
func (q *EdgePriorityQueue) Pop() interface{} {
	old := q.edges
	n := len(old)
	x := old[n-1]
	q.edges = old[:n-1]
	return x
}
func (q *EdgePriorityQueue) Top() *Edge {
	if len(q.edges) == 0 {
		return nil
	}
	return q.edges[0]
}
func (q *EdgePriorityQueue) Clear() {
	q.edges = nil
}
