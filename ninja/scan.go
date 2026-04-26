package main

type DependencyScan struct {
	state           *State
	buildLog        *BuildLog
	depsLog         *DepsLog
	disk_interface_ FileSystem
	depLoader       *ImplicitDepLoader
	dyndepLoader    *DyndepLoader
	explanations_   *Explanations
}

func NewDependencyScan(state *State, buildLog *BuildLog, depsLog *DepsLog,
	disk_interface FileSystem,
	depfile_parser_options *DepfileParserOptions, explanations *Explanations) *DependencyScan {
	return &DependencyScan{
		state:           state,
		buildLog:        buildLog,
		depsLog:         depsLog,
		disk_interface_: disk_interface,
		depLoader:       NewImplicitDepLoader(state, depsLog, disk_interface, depfile_parser_options, explanations),
		dyndepLoader:    NewDyndepLoader(state, disk_interface),
		explanations_:   explanations,
	}
}

func (s *DependencyScan) RecomputeDirty(initialNode *Node, validationNodes *[]*Node, err *string) bool {
	// queue of nodes to process
	nodes := []*Node{initialNode}

	for len(nodes) > 0 {
		node := nodes[0]
		nodes = nodes[1:]

		var stack []*Node
		var newValidationNodes []*Node

		if !s.RecomputeNodeDirty(node, &stack, &newValidationNodes, err) {
			return false
		}

		// append new validation nodes to the queue
		nodes = append(nodes, newValidationNodes...)

		if len(newValidationNodes) > 0 {
			if validationNodes == nil {
				panic("validations require RecomputeDirty to be called with validationNodes")
			}
			*validationNodes = append(*validationNodes, newValidationNodes...)
		}
	}

	return true
}

func (ds *DependencyScan) RecomputeNodeDirty(node *Node, stack *[]*Node, validationNodes *[]*Node, err *string) bool {
	edge := node.in_edge()
	if edge == nil {
		// If we already visited this leaf node then we are done.
		if node.StatusKnown() {
			return true
		}
		// This node has no in-edge_; it is dirty if it is missing.
		if !node.StatIfNecessary(ds.disk_interface_, err) {
			return false
		}
		if !node.Exists() {
			ds.explanations_.Record(node, "%s has no in-edge_ and is missing", node.path_)
		}
		node.SetDirty(!node.Exists())
		return true
	}

	// If we already finished this edge_ then we are done.
	if edge.mark_ == VisitDone {
		return true
	}

	// If we encountered this edge_ earlier in the call stack we have a cycle.
	if !ds.VerifyDAG(node, stack, err) {
		return false
	}

	// Store any validation nodes from the edge_ for adding to the initial nodes.
	// Don't recurse into them, that would trigger the dependency cycle detector
	// if the validation node depends on this node.
	// RecomputeDirty will add the validation nodes to the initial nodes and recurse into them.
	*validationNodes = append(*validationNodes, edge.validations_...)

	// mark_ the edge_ temporarily while in the call stack.
	edge.mark_ = VisitInStack
	*stack = append(*stack, node)

	dirty := false
	edge.outputs_ready_ = true
	edge.deps_missing_ = false

	if !edge.deps_loaded_ {
		// This is our first encounter with this edge_.
		edge.deps_loaded_ = true

		// If there is a pending dyndep log_file_, visit it now.
		if edge.dyndep_ != nil && edge.dyndep_.dyndep_pending_ {
			if !ds.RecomputeNodeDirty(edge.dyndep_, stack, validationNodes, err) {
				return false
			}
			if edge.dyndep_.in_edge() == nil || edge.dyndep_.in_edge().outputs_ready_ {
				// The dyndep log_file_ is ready, so load it now.
				if !ds.LoadDyndeps(edge.dyndep_, err) {
					return false
				}
			}
		}

		// Load discovered deps_.
		if !ds.depLoader.LoadDeps(edge, err) {
			if *err != "" {
				return false
			}
			// Failed to load dependency info: rebuild to regenerate it.
			// LoadDeps() did explanations_.Record already, no need to do it here.
			dirty = true
			edge.deps_missing_ = true
		}
	}

	// Visit all inputs before checking if any of them is ready.
	// Newly encountered edges may load dyndep files and gain outputs that correspond to some of our inputs.
	for _, i := range edge.inputs_ {
		if !ds.RecomputeNodeDirty(i, stack, validationNodes, err) {
			return false
		}
	}

	// Load output mtimes so we can compare them to the most recent input below.
	for _, o := range edge.outputs_ {
		if err != nil {
			*err = ""
		}
		if !o.StatIfNecessary(ds.disk_interface_, err) {
			return false
		}
	}

	// We're dirty if any of the inputs is dirty.
	var mostRecentInput *Node
	for idx, i := range edge.inputs_ {
		// If an input is not ready, neither are our outputs.
		if inEdge := i.in_edge(); inEdge != nil {
			if !inEdge.outputs_ready_ {
				edge.outputs_ready_ = false
			}
		}

		if !edge.is_order_only(idx) {
			// If a regular input is dirty (or missing), we're dirty.
			// Otherwise consider mtime.
			if i.Dirty() {
				ds.explanations_.Record(node, "%s is dirty", i.path_)
				dirty = true
			} else {
				if mostRecentInput == nil || i.mtime_ > mostRecentInput.mtime_ {
					mostRecentInput = i
				}
			}
		}
	}

	// We may also be dirty due to output state_: missing outputs, out of date outputs, etc.
	if !dirty {
		if !ds.RecomputeOutputsDirty(edge, mostRecentInput, &dirty, err) {
			return false
		}
	}

	// Finally, visit each output and update their dirty state_ if necessary.
	for _, o := range edge.outputs_ {
		if dirty {
			o.MarkDirty()
		}
	}

	// If an edge_ is dirty, its outputs are normally not ready.
	// (It's possible to be clean but still not be ready in the presence of order-only inputs.)
	// But phony edges with no inputs have nothing to do, so are always ready.
	if dirty && !(edge.IsPhony() && len(edge.inputs_) == 0) {
		edge.outputs_ready_ = false
	}

	// mark_ the edge_ as finished during this walk now that it will no longer be in the call stack.
	edge.mark_ = VisitDone
	if (*stack)[len(*stack)-1] != node {
		panic("assertion failed: stack back is not node")
	}
	*stack = (*stack)[:len(*stack)-1]

	return true
}

func (s *DependencyScan) LoadDyndeps(node *Node, err *string) bool {
	return s.dyndepLoader.LoadDyndeps(node, err)
}

func (s *DependencyScan) LoadDyndeps2(node *Node, ddf *DyndepFile, err *string) bool {
	return s.dyndepLoader.loadDyndeps(node, ddf, err)
}

func (s *DependencyScan) VerifyDAG(node *Node, stack *[]*Node, err *string) bool {
	edge := node.in_edge()
	if edge == nil {
		panic("assertion failed: edge_ != nil")
	}

	// If we have no temporary mark on the edge_ then we do not yet have a cycle.
	if edge.mark_ != VisitInStack {
		return true
	}

	// We have this edge_ earlier in the call stack. Find it.
	startIdx := -1
	for i, n := range *stack {
		if n.in_edge() == edge {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		panic("assertion failed: start != stack.end()")
	}

	// Make the cycle clear by reporting its start as the node at its end
	// instead of some other output of the starting edge_.
	(*stack)[startIdx] = node

	// Construct the error message rejecting the cycle.
	*err = "dependency cycle: "
	for i := startIdx; i < len(*stack); i++ {
		*err += (*stack)[i].path_
		*err += " -> "
	}
	*err += (*stack)[startIdx].path_

	if startIdx+1 == len(*stack) && edge.MaybePhonyCycleDiagnostic() {
		// The manifest parser would have filtered out the self-referencing
		// input if it were not configured to allow the error.
		*err += " [-w phonycycle=err]"
	}

	return false
}

func (s *DependencyScan) RecomputeOutputsDirty(edge *Edge, mostRecentInput *Node, outputs_dirty *bool, err *string) bool {
	command := edge.EvaluateCommand( /*incl_rsp_file=*/ true)
	for _, out := range edge.outputs_ {
		if s.RecomputeOutputDirty(edge, mostRecentInput, command, out) {
			*outputs_dirty = true
			return true
		}
	}
	return false
}

// RecomputeOutputDirty 判断单个输出节点是否需要重新构建（是否脏）。
// 参数 edge_ 是产生该输出的边，mostRecentInput 是最近修改的输入节点，
// command 是边的完整命令（用于比较命令哈希），output 是输出节点。
// 返回 true 表示需要重新构建，false 表示 clean。
func (ds *DependencyScan) RecomputeOutputDirty(edge *Edge, mostRecentInput *Node, command string, output *Node) bool {
	if edge.IsPhony() {
		// Phony edges don't write any output. outputs_ are only dirty if
		// there are no inputs or validations and we're missing the output.
		// If a phony target has inputs or validations, or the output exists,
		// they are used for dirty calculation instead of this fallback.
		if len(edge.inputs_) == 0 && len(edge.validations_) == 0 && !output.Exists() {
			ds.explanations_.Record(
				output, "output %s of phony edge_ with no inputs doesn't exist",
				output.path_)
			return true
		}

		// Update the mtime with the newest input. Dependents can thus call mtime()
		// on the fake node and get the latest mtime of the dependencies
		if mostRecentInput != nil {
			output.UpdatePhonyMtime(mostRecentInput.mtime_)
		}

		// Phony edges are clean, nothing to do
		return false
	}

	// Dirty if we're missing the output.
	if !output.Exists() {
		ds.explanations_.Record(output, "output %s doesn't exist",
			output.path_)
		return true
	}

	var entry *LogEntry

	// If this is a restat rule, we may have cleaned the output in a
	// previous run and stored the command start time in the build log.
	// We don't want to consider a restat rule's outputs as dirty unless
	// an input changed since the last run, so we'll skip checking the
	// output log_file_'s actual mtime and simply check the recorded mtime from
	// the log against the most recent input's mtime (see below)
	usedRestat := false
	if edge.GetBindingBool("restat") && ds.buildLog != nil {
		entry = ds.buildLog.LookupByOutput(output.path_)
		if entry != nil {
			usedRestat = true
		}
	}

	// Dirty if the output is older than the input.
	if !usedRestat && mostRecentInput != nil && output.mtime_ < mostRecentInput.mtime_ {
		ds.explanations_.Record(output,
			"output %s older than most recent input %s (%d vs %d)",
			output.path_,
			mostRecentInput.path_, output.mtime_,
			mostRecentInput.mtime_)
		return true
	}

	if ds.buildLog != nil {
		generator := edge.GetBindingBool("generator")
		if entry == nil {
			entry = ds.buildLog.LookupByOutput(output.path_)
		}
		if entry != nil {
			if !generator && HashCommand(command) != entry.CommandHash {
				// May also be dirty due to the command changing since the last build.
				// But if this is a generator rule, the command changing does not make us dirty.
				ds.explanations_.Record(output, "command line changed for %s",
					output.path_)
				return true
			}
			if mostRecentInput != nil && entry.Mtime < mostRecentInput.mtime_ {
				// May also be dirty due to the mtime in the log being older than the
				// mtime of the most recent input. This can occur even when the mtime
				// on disk_interface_ is newer if a previous run wrote to the output log_file_ but
				// exited with an error or was interrupted. If this was a restat rule,
				// then we only check the recorded mtime against the most recent input
				// mtime and ignore the actual output's mtime above.
				ds.explanations_.Record(output,
					"recorded mtime of %s older than most recent input %s (%d vs %d)",
					output.path_, mostRecentInput.path_,
					entry.Mtime, mostRecentInput.mtime_)
				return true
			}
		}
		if entry == nil && !generator {
			ds.explanations_.Record(output, "command line not found in log for %s",
				output.path_)
			return true
		}
	}

	return false
}
