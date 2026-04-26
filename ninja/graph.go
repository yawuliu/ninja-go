package main

type ImplicitDepLoader struct {
	state_                  *State
	deps_log_               *DepsLog
	disk_interface_         FileSystem
	depfile_parser_options_ *DepfileParserOptions // 可忽略
	explanations_           *Explanations
}

func NewImplicitDepLoader(state *State, depsLog *DepsLog, disk_interface FileSystem,
	depfile_parser_options *DepfileParserOptions, explanations *Explanations) *ImplicitDepLoader {
	return &ImplicitDepLoader{
		state_:                  state,
		deps_log_:               depsLog,
		disk_interface_:         disk_interface,
		depfile_parser_options_: depfile_parser_options,
		explanations_:           explanations,
	}
}

func (l *ImplicitDepLoader) LoadDeps(edge *Edge, err *string) bool {
	depsType := edge.GetBinding("deps_")
	if depsType != "" {
		return l.LoadDepsFromLog(edge, err)
	}

	depfile := edge.GetUnescapedDepfile()
	if depfile != "" {
		return l.LoadDepFile(edge, depfile, err)
	}

	// No deps_ to load.
	return true
}

func (l *ImplicitDepLoader) LoadDepsFromLog(edge *Edge, err *string) bool {
	// NOTE: deps_ are only supported for single-target edges.
	output := edge.outputs_[0]
	var deps *Deps
	if l.deps_log_ != nil {
		deps = l.deps_log_.GetDeps(output)
	}
	if deps == nil {
		l.explanations_.Record(output, "deps_ for '%s' are missing",
			output.path_)
		return false
	}

	// Load the output's mtime if we haven't already.
	if !output.StatIfNecessary(l.disk_interface_, err) {
		return false
	}

	// Deps are invalid if the output is newer than the deps_.
	if output.mtime_ > deps.mtime {
		l.explanations_.Record(output,
			"stored deps_ info out of date for '%s' (%d vs %d)",
			output.path_, deps.mtime, output.mtime_)
		return false
	}

	nodes := deps.nodes
	nodeCount := deps.node_count
	// Insert nodes_ before the order-only dependencies
	insertPos := len(edge.inputs_) - edge.order_only_deps_
	edge.inputs_ = append(edge.inputs_[:insertPos], append(nodes, edge.inputs_[insertPos:]...)...)
	edge.implicit_deps_ += nodeCount
	for i := 0; i < nodeCount; i++ {
		nodes[i].AddOutEdge(edge)
	}
	return true
}

func (l *ImplicitDepLoader) LoadDepFile(edge *Edge, path string, err *string) bool {
	// METRIC_RECORD("depfile load") - ignored

	// Read depfile content. Treat a missing depfile as empty.
	var content string
	status := l.disk_interface_.ReadFile(path, &content, err)
	if status == StatusNotFound {
		*err = "" // clear error
	} else if status == StatusOtherError {
		*err = "loading '" + path + "': " + *err
		return false
	}

	firstOutput := edge.outputs_[0]
	if content == "" {
		l.explanations_.Record(firstOutput, "depfile '%s' is missing", path)
		return false
	}

	depfileParser := NewDepfileParser(l.depfile_parser_options_)
	depfileErr := ""
	if !depfileParser.Parse(content, &depfileErr) {
		*err = path + ": " + depfileErr
		return false
	}

	if len(depfileParser.Outs) == 0 {
		*err = path + ": no outputs declared"
		return false
	}

	// Canonicalize the primary output path.
	primaryOut := depfileParser.Outs[0]
	primaryOutLen := len(primaryOut)
	var canonicalized []byte
	var unused uint64
	CanonicalizePathBytes(canonicalized, &primaryOutLen, &unused)
	// Update the string slice (depfileParser.outs is a slice of strings, we need to replace)
	depfileParser.Outs[0] = string(canonicalized)

	// Check that this depfile matches the edge_'s output.
	if firstOutput.path_ != string(canonicalized) {
		l.explanations_.Record(firstOutput,
			"expected depfile '%s' to mention '%s', got '%s'",
			path, firstOutput.path_, string(canonicalized))
		return false
	}

	// Ensure that all mentioned outputs are outputs of the edge_.
	for _, o := range depfileParser.Outs {
		found := false
		for _, edgeOut := range edge.outputs_ {
			if edgeOut.path_ == o {
				found = true
				break
			}
		}
		if !found {
			*err = path + ": depfile mentions '" + o + "' as an output, but no such output was declared"
			return false
		}
	}

	return l.ProcessDepfileDeps(edge, depfileParser.Ins, err)
}

func (l *ImplicitDepLoader) ProcessDepfileDeps(edge *Edge, depfileIns []string, err *string) bool {
	// Preallocate space in edge_.inputs for the new implicit dependencies.
	// In Go, we can simply extend the slice and fill.
	startIdx := len(edge.inputs_) - edge.order_only_deps_
	// Make room for len(depfileIns) new items at the insertion point.
	edge.inputs_ = append(edge.inputs_[:startIdx], append(make([]*Node, len(depfileIns)), edge.inputs_[startIdx:]...)...)

	// Add all nodes_ as implicit dependencies.
	for i, path := range depfileIns {
		// Canonicalize the path and get slash bits.
		var slash_bits uint64
		pathBytes := []byte(path)
		pathBytesLen := len(pathBytes)
		CanonicalizePathBytes(pathBytes, &pathBytesLen, &slash_bits)
		node := l.state_.GetNode(string(pathBytes), slash_bits)
		// Store the node in the preallocated position.
		edge.inputs_[startIdx+i] = node
		node.AddOutEdge(edge)
	}
	edge.implicit_deps_ += len(depfileIns)

	return true
}
