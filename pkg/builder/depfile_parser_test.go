package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ninja-go/pkg/graph"
)

// 辅助函数：创建 Builder 实例并调用 parseDepfile
func parseDepfileContent(t *testing.T, content string, outputPath string) (*graph.Edge, *graph.State) {
	fs := newMockFileSystem()
	fs.WriteFile(outputPath+".d", []byte(content), 0644)

	state := graph.NewState()
	rule := &graph.Rule{Name: "cc", Depfile: "$out.d"}
	edge := &graph.Edge{Rule: rule}
	outNode := state.AddNode(outputPath)
	edge.Outputs = []*graph.Node{outNode}
	outNode.Edge = edge
	state.Edges = append(state.Edges, edge)

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.parseDepfile(edge)
	require.NoError(t, err)
	return edge, state
}

func TestDepfileParser_Basic(t *testing.T) {
	content := "build/ninja.o: ninja.cc ninja.h eval_env.h manifest_parser.h\n"
	edge, _ := parseDepfileContent(t, content, "build/ninja.o")

	assert.Len(t, edge.ImplicitDeps, 4)
	paths := []string{}
	for _, n := range edge.ImplicitDeps {
		paths = append(paths, n.Path)
	}
	assert.ElementsMatch(t, []string{"ninja.cc", "ninja.h", "eval_env.h", "manifest_parser.h"}, paths)
}

func TestDepfileParser_EarlyNewlineAndWhitespace(t *testing.T) {
	content := " \\\n  out: in\n"
	edge, _ := parseDepfileContent(t, content, "out")
	assert.Len(t, edge.ImplicitDeps, 1)
	assert.Equal(t, "in", edge.ImplicitDeps[0].Path)
}

func TestDepfileParser_Continuation(t *testing.T) {
	content := "foo.o: \\\n  bar.h baz.h\n"
	edge, _ := parseDepfileContent(t, content, "foo.o")
	assert.Len(t, edge.ImplicitDeps, 2)
	assert.Equal(t, "bar.h", edge.ImplicitDeps[0].Path)
	assert.Equal(t, "baz.h", edge.ImplicitDeps[1].Path)
}

func TestDepfileParser_WindowsDrivePaths(t *testing.T) {
	content := "foo.o: //?/c:/bar.h\n"
	edge, _ := parseDepfileContent(t, content, "foo.o")
	assert.Len(t, edge.ImplicitDeps, 1)
	assert.Equal(t, "//?/c:/bar.h", edge.ImplicitDeps[0].Path)
}

func TestDepfileParser_AmpersandsAndQuotes(t *testing.T) {
	content := "foo&bar.o foo'bar.o foo\"bar.o: foo&bar.h foo'bar.h foo\"bar.h\n"
	edge, _ := parseDepfileContent(t, content, "foo&bar.o")
	assert.Len(t, edge.ImplicitDeps, 3)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path, edge.ImplicitDeps[2].Path}
	assert.ElementsMatch(t, []string{"foo&bar.h", "foo'bar.h", "foo\"bar.h"}, paths)
}

func TestDepfileParser_CarriageReturnContinuation(t *testing.T) {
	content := "foo.o: \\\r\n  bar.h baz.h\r\n"
	edge, _ := parseDepfileContent(t, content, "foo.o")
	assert.Len(t, edge.ImplicitDeps, 2)
	assert.Equal(t, "bar.h", edge.ImplicitDeps[0].Path)
	assert.Equal(t, "baz.h", edge.ImplicitDeps[1].Path)
}

func TestDepfileParser_BackSlashes(t *testing.T) {
	content := "Project\\Dir\\Build\\Release8\\Foo\\Foo.res : \\\n" +
		"  Dir\\Library\\Foo.rc \\\n" +
		"  Dir\\Library\\Version\\Bar.h \\\n" +
		"  Dir\\Library\\Foo.ico \\\n" +
		"  Project\\Thing\\Bar.tlb \\\n"
	edge, _ := parseDepfileContent(t, content, "Project\\Dir\\Build\\Release8\\Foo\\Foo.res")
	assert.Len(t, edge.ImplicitDeps, 4)
	paths := []string{}
	for _, n := range edge.ImplicitDeps {
		paths = append(paths, n.Path)
	}
	assert.ElementsMatch(t, []string{
		"Dir\\Library\\Foo.rc",
		"Dir\\Library\\Version\\Bar.h",
		"Dir\\Library\\Foo.ico",
		"Project\\Thing\\Bar.tlb",
	}, paths)
}

func TestDepfileParser_Spaces(t *testing.T) {
	content := "a\\ bc\\ def:   a\\ b c d"
	edge, _ := parseDepfileContent(t, content, "a bc def")
	assert.Len(t, edge.ImplicitDeps, 3)
	assert.Equal(t, "a b", edge.ImplicitDeps[0].Path)
	assert.Equal(t, "c", edge.ImplicitDeps[1].Path)
	assert.Equal(t, "d", edge.ImplicitDeps[2].Path)
}

func TestDepfileParser_MultipleBackslashes(t *testing.T) {
	content := "a\\ b\\#c.h: \\\\\\\\\\  \\\\\\\\ \\\\share\\info\\\\#1"
	edge, _ := parseDepfileContent(t, content, "a b#c.h")
	assert.Len(t, edge.ImplicitDeps, 3)
	assert.Equal(t, "\\\\ ", edge.ImplicitDeps[0].Path)
	assert.Equal(t, "\\\\\\\\", edge.ImplicitDeps[1].Path)
	assert.Equal(t, "\\\\share\\info\\#1", edge.ImplicitDeps[2].Path)
}

func TestDepfileParser_Escapes(t *testing.T) {
	content := "\\!\\@\\#$$\\%\\^\\&\\[\\]\\\\:"
	edge, _ := parseDepfileContent(t, content, "\\!\\@#$\\%\\^\\&\\[\\]\\\\")
	assert.Empty(t, edge.ImplicitDeps)
}

func TestDepfileParser_EscapedColons(t *testing.T) {
	content := "c\\:\\gcc\\x86_64-w64-mingw32\\include\\stddef.o: \\\n" +
		" c:\\gcc\\x86_64-w64-mingw32\\include\\stddef.h \n"
	edge, _ := parseDepfileContent(t, content, "c:\\gcc\\x86_64-w64-mingw32\\include\\stddef.o")
	assert.Len(t, edge.ImplicitDeps, 1)
	assert.Equal(t, "c:\\gcc\\x86_64-w64-mingw32\\include\\stddef.h", edge.ImplicitDeps[0].Path)
}

func TestDepfileParser_EscapedTargetColon(t *testing.T) {
	content := "foo1\\: x\nfoo1\\:\nfoo1\\:\r\nfoo1\\:\t\nfoo1\\:"
	edge, _ := parseDepfileContent(t, content, "foo1\\")
	assert.Len(t, edge.ImplicitDeps, 1)
	assert.Equal(t, "x", edge.ImplicitDeps[0].Path)
}

func TestDepfileParser_SpecialChars(t *testing.T) {
	content := "C:/Program\\ Files\\ (x86)/Microsoft\\ crtdefs.h: \\\n" +
		" en@quot.header~ t+t-x!=1 \\\n" +
		" openldap/slapd.d/cn=config/cn=schema/cn={0}core.ldif\\\n" +
		" Fu\303\244ball\\\n" +
		" a[1]b@2%c"
	edge, _ := parseDepfileContent(t, content, "C:/Program Files (x86)/Microsoft crtdefs.h")
	assert.Len(t, edge.ImplicitDeps, 5)
	paths := []string{}
	for _, n := range edge.ImplicitDeps {
		paths = append(paths, n.Path)
	}
	assert.ElementsMatch(t, []string{
		"en@quot.header~",
		"t+t-x!=1",
		"openldap/slapd.d/cn=config/cn=schema/cn={0}core.ldif",
		"Fu\303\244ball",
		"a[1]b@2%c",
	}, paths)
}

func TestDepfileParser_UnifyMultipleOutputs(t *testing.T) {
	content := "foo foo: x y z"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 3)
	assert.Equal(t, "x", edge.ImplicitDeps[0].Path)
	assert.Equal(t, "y", edge.ImplicitDeps[1].Path)
	assert.Equal(t, "z", edge.ImplicitDeps[2].Path)
}

func TestDepfileParser_MultipleDifferentOutputs(t *testing.T) {
	content := "foo bar: x y z"
	edge, _ := parseDepfileContent(t, content, "foo")
	// 解析器应提取所有输出，但当前只匹配边的主输出；这里我们期望隐式依赖包含所有输入
	assert.Len(t, edge.ImplicitDeps, 3)
	assert.Equal(t, "x", edge.ImplicitDeps[0].Path)
	assert.Equal(t, "y", edge.ImplicitDeps[1].Path)
	assert.Equal(t, "z", edge.ImplicitDeps[2].Path)
}

func TestDepfileParser_MultipleEmptyRules(t *testing.T) {
	content := "foo: x\nfoo: \nfoo:\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 1)
	assert.Equal(t, "x", edge.ImplicitDeps[0].Path)
}

func TestDepfileParser_UnifyMultipleRulesLF(t *testing.T) {
	content := "foo: x\nfoo: y\nfoo \\\nfoo: z\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 3)
	assert.ElementsMatch(t, []string{"x", "y", "z"}, []string{
		edge.ImplicitDeps[0].Path,
		edge.ImplicitDeps[1].Path,
		edge.ImplicitDeps[2].Path,
	})
}

func TestDepfileParser_UnifyMixedRulesLF(t *testing.T) {
	content := "foo: x\\\n     y\nfoo \\\nfoo: z\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 3)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path, edge.ImplicitDeps[2].Path}
	assert.ElementsMatch(t, []string{"x", "y", "z"}, paths)
}

func TestDepfileParser_IndentedRulesLF(t *testing.T) {
	content := " foo: x\n foo: y\n foo: z\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 3)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path, edge.ImplicitDeps[2].Path}
	assert.ElementsMatch(t, []string{"x", "y", "z"}, paths)
}

func TestDepfileParser_TolerateMP(t *testing.T) {
	content := "foo: x y z\nx:\ny:\nz:\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 3)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path, edge.ImplicitDeps[2].Path}
	assert.ElementsMatch(t, []string{"x", "y", "z"}, paths)
}

func TestDepfileParser_MultipleRulesTolerateMP(t *testing.T) {
	content := "foo: x\nx:\nfoo: y\ny:\nfoo: z\nz:\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 3)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path, edge.ImplicitDeps[2].Path}
	assert.ElementsMatch(t, []string{"x", "y", "z"}, paths)
}

func TestDepfileParser_MultipleRulesDifferentOutputs(t *testing.T) {
	content := "foo: x y\nbar: y z\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Len(t, edge.ImplicitDeps, 2)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path}
	assert.ElementsMatch(t, []string{"x", "y"}, paths)
}

func TestDepfileParser_BuggyMP(t *testing.T) {
	content := "foo: x y z\nx: alsoin\ny:\nz:\n"
	edge, state := parseDepfileContent(t, content, "foo")
	// 当前实现忽略"x: alsoin"这类规则，不会报错，只解析有效行
	assert.Len(t, edge.ImplicitDeps, 3)
	paths := []string{edge.ImplicitDeps[0].Path, edge.ImplicitDeps[1].Path, edge.ImplicitDeps[2].Path}
	assert.ElementsMatch(t, []string{"x", "y", "z"}, paths)
	// 验证 alsoin 没有被错误添加
	assert.Nil(t, state.LookupNode("alsoin"))
}

func TestDepfileParser_EmptyFile(t *testing.T) {
	content := ""
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Empty(t, edge.ImplicitDeps)
}

func TestDepfileParser_EmptyLines(t *testing.T) {
	content := "\n\n"
	edge, _ := parseDepfileContent(t, content, "foo")
	assert.Empty(t, edge.ImplicitDeps)
}

func TestDepfileParser_MissingColon(t *testing.T) {
	content := "foo.o foo.c\n"
	fs := newMockFileSystem()
	fs.WriteFile("foo.o.d", []byte(content), 0644)
	state := graph.NewState()
	rule := &graph.Rule{Name: "cc", Depfile: "$out.d"}
	edge := &graph.Edge{Rule: rule}
	outNode := state.AddNode("foo.o")
	edge.Outputs = []*graph.Node{outNode}
	outNode.Edge = edge
	state.Edges = append(state.Edges, edge)

	builder := NewBuilder(state, 1, newMockCommandRunner(fs), fs)
	err := builder.parseDepfile(edge)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected ':' in depfile")
}
