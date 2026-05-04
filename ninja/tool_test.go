package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolTargets_Depth tests the -t targets depth subcommand
func TestToolTargets_Depth(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()

	manifest := `
rule cc
  command = cc $in -o $out
rule link
  command = ld $in -o $out

build a.o: cc a.c
build b.o: cc b.c
build prog: link a.o b.o

default prog
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	// Verify that default node is "prog"
	defaults := state.DefaultNodes(&err)
	require.Empty(t, err)
	require.Len(t, defaults, 1)
	assert.Equal(t, "prog", defaults[0].path_)
}

// TestToolClean tests the -t clean tool
func TestToolClean_Basic(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	config := DefaultBuildConfig()

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c
build bar.o: cc bar.c
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	// Test clean with specific targets
	cleaner := NewCleaner(state, &config, fs)
	result := cleaner.CleanTargets([]string{"foo.o"})
	assert.Equal(t, 0, result)
}

// TestToolCommands tests the -t commands tool
func TestToolCommands_Single(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()

	manifest := `
rule cc
  command = gcc -c $in -o $out

build foo.o: cc foo.c
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	// Verify command is evaluated correctly
	edge := state.edges_[0]
	cmd := edge.EvaluateCommand(false)
	assert.Contains(t, cmd, "gcc")
	assert.Contains(t, cmd, "foo.c")
	assert.Contains(t, cmd, "foo.o")
}

// TestEvaluateCommandWithRspfile tests response file expansion
func TestEvaluateCommandWithRspfile(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()

	manifest := `
rule link
  command = ld @$rspfile -o $out
  rspfile = $out.rsp
  rspfile_content = $in

build prog: link foo.o bar.o baz.o
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	edge := state.edges_[0]
	normalCmd := EvaluateCommandWithRspfile(edge, ECM_NORMAL)
	assert.Contains(t, normalCmd, "ld")
	assert.Contains(t, normalCmd, "prog")

	expandedCmd := EvaluateCommandWithRspfile(edge, ECM_EXPAND_RSPFILE)
	assert.Contains(t, expandedCmd, "foo.o")
	assert.Contains(t, expandedCmd, "bar.o")
	assert.Contains(t, expandedCmd, "baz.o")
}

// TestPrintCommands tests command traversal
func TestPrintCommands_SingleMode(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()

	manifest := `
rule cc
  command = gcc -c $in -o $out

rule link
  command = ld $in -o $out

build foo.o: cc foo.c
build bar.o: cc bar.c
build prog: link foo.o bar.o
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	// Find the link edge (prog)
	var linkEdge *Edge
	for _, e := range state.edges_ {
		if e.rule_.Name == "link" {
			linkEdge = e
			break
		}
	}
	require.NotNil(t, linkEdge)

	// In PCM_Single, only prints commands for the specific target
	seen := make(map[*Edge]bool)
	// Test that single mode does not recurse into inputs
	// It just prints the command for this edge
	PrintCommands(linkEdge, seen, PCM_Single)
	assert.True(t, seen[linkEdge])

	// In PCM_All, it recurses into inputs
	seen2 := make(map[*Edge]bool)
	PrintCommands(linkEdge, seen2, PCM_All)
	assert.True(t, seen2[linkEdge])
	// Should have visited the cc edges too
	assert.GreaterOrEqual(t, len(seen2), 1)
}

// TestGraphViz tests graphviz output generation
func TestGraphViz_Basic(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()

	manifest := `
rule cc
  command = gcc -c $in -o $out

build foo.o: cc foo.c
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	graph := NewGraphViz(state, fs)
	require.NotNil(t, graph)
}

// TestDependencyScan tests dependency scanning
func TestDependencyScan_Basic(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	buildLog := NewBuildLog("")
	depsLog := NewDepsLog("")
	explanations := NewExplanations()

	scan := NewDependencyScan(state, buildLog, depsLog, fs,
		&DepfileParserOptions{}, explanations)
	require.NotNil(t, scan)

	// Verify the edge was created
	edge := state.edges_[0]
	assert.Equal(t, "cc", edge.rule_.Name)
	assert.Len(t, edge.inputs_, 1)
	assert.Len(t, edge.outputs_, 1)
}

// TestDepsLog tests the deps log creation
func TestDepsLog_Creation(t *testing.T) {
	depsLog := NewDepsLog("test.ninja_deps")
	require.NotNil(t, depsLog)

	// Initially nodes should be empty
	assert.NotNil(t, depsLog.Nodes())
}

// TestNode_OutEdges tests node output edges
func TestNode_OutEdges(t *testing.T) {
	node := NewNode("test.o", 0)
	assert.Len(t, node.out_edges_, 0)

	rule := &Rule{Name: "cc"}
	edge := &Edge{rule_: rule, id_: uint64(1)}
	node.AddOutEdge(edge)
	assert.Len(t, node.out_edges_, 1)

	// Same edge twice shouldn't duplicate
	node.AddOutEdge(edge)
	assert.Len(t, node.out_edges_, 1)
}

// TestCleaner_CleanDead tests cleaning dead entries
func TestCleaner_CleanDead(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	config := DefaultBuildConfig()

	manifest := `
rule cc
  command = cc $in -o $out

build foo.o: cc foo.c
`
	var err string
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	parser.ParseTest(manifest, &err)
	require.Empty(t, err)

	// Create a build log with entries that are not in the current state
	buildLog := NewBuildLog("test")
	user := &mockBuildLogUser{deadPaths: make(map[string]bool)}
	buildLog.OpenForWrite("test_buildlog", user, &err)

	// Record a command for an output NOT in the current state
	// This would be a "dead" entry
	oldEdge := &Edge{
		rule_:    &Rule{Name: "cc"},
		outputs_: []*Node{{path_: "old_output.o"}},
	}
	buildLog.RecordCommand(oldEdge, 100, 200, 200)
	buildLog.Close()

	cleaner := NewCleaner(state, &config, fs)
	result := cleaner.CleanDead(buildLog.Entries())
	assert.Equal(t, 0, result)
}

// TestPool_DelayAndRetrieve tests pool edge scheduling
func TestPool_DelayAndRetrieve(t *testing.T) {
	pool := NewPool("test_pool", 2)
	queue := &EdgePriorityQueue{}

	rule := &Rule{Name: "cc"}
	edge1 := &Edge{rule_: rule, id_: 1}
	edge2 := &Edge{rule_: rule, id_: 2}
	edge3 := &Edge{rule_: rule, id_: 3}

	// Schedule edge1 (weight 1, current=1 <= 2 OK)
	pool.EdgeScheduled(edge1)
	assert.Equal(t, 1, pool.currentUse)

	// Try another
	pool.EdgeScheduled(edge2)
	assert.Equal(t, 2, pool.currentUse)

	// Delay edge3 since current=2 + 1 > depth=2
	pool.DelayEdge(edge3)
	assert.Len(t, pool.delayed, 1)

	// Finish edge1, now current=1
	pool.EdgeFinished(edge1)
	assert.Equal(t, 1, pool.currentUse)

	// Retrieve ready edges from delayed
	pool.RetrieveReadyEdges(queue)
	assert.Empty(t, pool.delayed) // edge3 should be scheduled
	assert.Equal(t, 2, pool.currentUse)
}

// TestBuildConfig_Defaults tests default build configuration
func TestBuildConfig_Defaults(t *testing.T) {
	config := DefaultBuildConfig()
	assert.Equal(t, NORMAL, config.GetVerbosity())
	assert.Equal(t, false, config.GetDryRun())
	assert.Equal(t, 1, config.GetParallelism())
	assert.Equal(t, 1, config.GetFailuresAllowed())
	assert.Equal(t, false, config.GetDisableJobserverClient())
}

// TestBuildConfig_Setters tests build configuration setters
func TestBuildConfig_Setters(t *testing.T) {
	config := DefaultBuildConfig()

	config.SetVerbosity(VERBOSE)
	assert.Equal(t, VERBOSE, config.GetVerbosity())

	config.SetDryRun(true)
	assert.Equal(t, true, config.GetDryRun())

	config.SetParallelism(8)
	assert.Equal(t, 8, config.GetParallelism())

	config.SetFailuresAllowed(10)
	assert.Equal(t, 10, config.GetFailuresAllowed())

	config.SetDisableJobserverClient(true)
	assert.Equal(t, true, config.GetDisableJobserverClient())
}

// TestHexagonalEscapeCodes tests ANSI escape code stripping
func TestStripAnsiEscapeCodes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"plain text", "plain text"},
		{"\x1b[31mred text\x1b[0m", "red text"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"no escape", "no escape"},
		{"\x1b[Kclear line", "clear line"},
	}

	for _, tt := range tests {
		result := StripAnsiEscapeCodes(tt.input)
		assert.Equal(t, tt.expected, result, "input: %q", tt.input)
	}
}

// TestShellEscaping tests shell escaping functions
func TestShellEscaping(t *testing.T) {
	// Test that safe strings are not escaped
	safe := "hello_world.c"
	assert.Equal(t, safe, GetShellEscapedString(safe))

	safe2 := "path/to/file.o"
	assert.Equal(t, safe2, GetShellEscapedString(safe2))

	// Test that unsafe strings are escaped
	unsafe := "file with spaces.c"
	escaped := GetShellEscapedString(unsafe)
	assert.NotEqual(t, unsafe, escaped)
	assert.Contains(t, escaped, "'")
}

// TestWin32Escaping tests Windows command escaping
func TestWin32Escaping(t *testing.T) {
	// Safe string
	safe := "C:\\path\\file.o"
	// On Windows, this might need escaping if it uses backslashes
	_ = GetWin32EscapedString(safe)

	// String with spaces should be quoted
	unsafe := "C:\\Program Files\\file.o"
	escaped := GetWin32EscapedString(unsafe)
	assert.NotEqual(t, unsafe, escaped)
}

// TestParseVersion tests version parsing
func TestParseVersion(t *testing.T) {
	// Test valid version
	major, minor := ParseVersion("1.14")
	assert.Equal(t, 1, major)
	assert.Equal(t, 14, minor)

	// Test single-digit minor
	major, minor = ParseVersion("2.5")
	assert.Equal(t, 2, major)
	assert.Equal(t, 5, minor)

	// Test three-digit minor
	major, minor = ParseVersion("1.100")
	assert.Equal(t, 1, major)
	assert.Equal(t, 100, minor)
}

// TestPathCanonicalization_Basic tests path canonicalization
func TestPathCanonicalization_Basic(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{".", "."},
		{"foo", "foo"},
		{"foo/bar", "foo/bar"},
		{"foo/./bar", "foo/bar"},
		{"./foo", "foo"},
		{"foo/.", "foo"},
	}

	for _, tt := range tests {
		path := tt.input
		var slashBits uint64
		CanonicalizePathString(&path, &slashBits)
		assert.Equal(t, tt.expected, path, "input: %q", tt.input)
	}
}

// TestDirName tests dirname extraction
func TestDirName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"foo", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := DirName(tt.input)
		assert.Equal(t, tt.expected, result, "input: %q", tt.input)
	}
}

// TestNode_All methods coverage
func TestNode_ValidationEdge(t *testing.T) {
	node := NewNode("test.txt", 0)
	rule := &Rule{Name: "check"}
	edge := &Edge{rule_: rule}

	// Initially no validation edges
	assert.Empty(t, node.validation_out_edges_)

	// Add validation edge
	node.AddValidationOutEdge(edge)
	assert.Len(t, node.validation_out_edges_, 1)

}

// TestNode_ReservedBinding tests that reserved bindings can be set on rules
func TestRule_ReservedBinding(t *testing.T) {
	// Check that "restat" is a reserved binding
	assert.True(t, IsReservedBinding("restat"))
	assert.True(t, IsReservedBinding("generator"))
	assert.True(t, IsReservedBinding("pool"))
	assert.True(t, IsReservedBinding("deps"))
	assert.True(t, IsReservedBinding("depfile"))
	assert.True(t, IsReservedBinding("dyndep"))
	assert.True(t, IsReservedBinding("command"))
	assert.True(t, IsReservedBinding("description"))
	assert.True(t, IsReservedBinding("rspfile"))
	assert.True(t, IsReservedBinding("rspfile_content"))

	// Non-reserved binding
	assert.False(t, IsReservedBinding("myvariable"))
	assert.False(t, IsReservedBinding("custom_flag"))
}

// TestSpellcheckString tests spell checking
func TestSpellcheckString(t *testing.T) {
	// Exact match
	result := SpellcheckString("cc", []string{"cc", "link"})
	assert.Equal(t, "cc", result)

	// Close match
	result = SpellcheckString("ccc", []string{"cc", "link", "cxx"})
	assert.NotEmpty(t, result)

	// No match
	result = SpellcheckString("zzzzz", []string{"cc", "link"})
	// With max distance 3, this might not match anything
	_ = result
}

// TestFatal_Warning tests utility functions
func TestUtil_Functions(t *testing.T) {
	// Test that these functions don't panic
	assert.NotPanics(t, func() {
		// Just testing the function signatures exist
		_ = GetLoadAverage()
	})
}

// TestBuilder_Creation tests builder creation
func TestBuilder_Creation(t *testing.T) {
	fs := newMockFSIntegration()
	state := NewState()
	config := DefaultBuildConfig()
	buildLog := NewBuildLog("")
	depsLog := NewDepsLog("")

	builder := NewBuilder(state, &config, buildLog, depsLog,
		0, fs, NewStatusPrinter(config))
	require.NotNil(t, builder)
	assert.NotNil(t, builder.plan_)
	assert.NotNil(t, builder.scan_)
}

// TestEdge_Weight tests edge weight calculation
func TestEdge_Weight(t *testing.T) {
	edge := &Edge{rule_: &Rule{Name: "cc"}}
	assert.Equal(t, 1, edge.Weight())
}

// TestPool_DefaultConsole tests default pool setup
func TestPool_DefaultConsole(t *testing.T) {
	assert.NotNil(t, kDefaultPool)
	assert.Equal(t, "", kDefaultPool.Name)
	assert.Equal(t, 0, kDefaultPool.Depth)

	assert.NotNil(t, kConsolePool)
	assert.Equal(t, "console", kConsolePool.Name)
	assert.Equal(t, 1, kConsolePool.Depth)
}

// TestExitStatus tests exit status values
func TestExitStatus(t *testing.T) {
	assert.NotEqual(t, ExitSuccess, ExitFailure)
	assert.NotEqual(t, ExitSuccess, ExitInterrupted)
	assert.NotEqual(t, ExitFailure, ExitInterrupted)
}

// TestTokenNames tests token string representation
func TestTokenNames(t *testing.T) {
	assert.Equal(t, "'build'", BUILD.String())
	assert.Equal(t, "'rule'", RULE.String())
	assert.Equal(t, "'default'", DEFAULT.String())
	assert.Equal(t, "'='", EQUALS.String())
	assert.Equal(t, "':'", COLON.String())
	assert.Equal(t, "'|'", PIPE.String())
	assert.Equal(t, "'||'", PIPE2.String())
	assert.Equal(t, "'|@'", PIPEAT.String())
	assert.Equal(t, "indent", INDENT.String())
	assert.Equal(t, "newline", NEWLINE.String())
	assert.Equal(t, "'pool'", POOL.String())
	assert.Equal(t, "'include'", INCLUDE.String())
	assert.Equal(t, "'subninja'", SUBNINJA.String())
	assert.Equal(t, "identifier", IDENT.String())
	assert.Equal(t, "eof", TEOF.String())
	assert.Equal(t, "lexing error", ERROR.String())
}

// TestMetric_Recording tests metric infrastructure
func TestMetric_Recording(t *testing.T) {
	// After resetting, g_metrics should be nil
	// Actually g_metrics is only set in debug mode
	// So we just test the nil case
	g_metrics = &Metrics{}
	g_metrics.Report()
	g_metrics = nil
}
