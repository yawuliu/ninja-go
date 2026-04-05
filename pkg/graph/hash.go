package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func ComputeCommandHash(edge *Edge) string {
	cmd := edge.Rule.Command
	inPaths := make([]string, len(edge.Inputs))
	for i, in := range edge.Inputs {
		inPaths[i] = in.Path
	}
	outPaths := make([]string, len(edge.Outputs))
	for i, out := range edge.Outputs {
		outPaths[i] = out.Path
	}
	expanded := strings.ReplaceAll(cmd, "$in", strings.Join(inPaths, " "))
	expanded = strings.ReplaceAll(expanded, "$out", strings.Join(outPaths, " "))
	expanded += edge.Rule.Depfile + edge.Rule.Dyndep
	hash := sha256.Sum256([]byte(expanded))
	return hex.EncodeToString(hash[:])
}

// HashEdge 计算边的哈希（需要导入 graph 包，这里假设已实现）
func HashEdge(edge *Edge) string {
	// 临时实现：根据规则名和输入输出计算
	parts := []string{edge.Rule.Name}
	for _, in := range edge.Inputs {
		parts = append(parts, in.Path)
	}
	for _, out := range edge.Outputs {
		parts = append(parts, out.Path)
	}
	parts = append(parts, edge.Rule.Command)
	return strings.Join(parts, "|") // 实际应 hash
}
