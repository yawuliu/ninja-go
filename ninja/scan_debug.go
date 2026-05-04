package main

import "fmt"

// debugDirty prints when a node/edge is marked dirty.
func debugDirty(reason string, node *Node, edge *Edge) {
	if edge != nil && len(edge.outputs_) > 0 {
		fmt.Printf("[dirty] %-40s output=%-30s dirty=%v\n",
			reason, edge.outputs_[0].path_, node.dirty_)
	} else {
		fmt.Printf("[dirty] %-40s path=%-30s dirty=%v\n",
			reason, node.path_, node.dirty_)
	}
}
