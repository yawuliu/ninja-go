#!/bin/bash
# ============================================================
#  ninja-go Release Script
#  Cross-compile and create a GitHub release
#
#  Usage:
#    ./scripts/release.sh <version>
#
#  Example:
#    ./scripts/release.sh v0.1.0
#
#  Requires: Go, git, gh (GitHub CLI)
# ============================================================
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.1.0"
    exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

LDFLAGS="-s -w -X main.kNinjaVersion=$VERSION"
BUILD_DIR="$ROOT_DIR/_release"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

# ---- Check prerequisites ----
command -v go  >/dev/null 2>&1 || { echo "ERROR: go not found"; exit 1; }
command -v git >/dev/null 2>&1 || { echo "ERROR: git not found"; exit 1; }
command -v gh  >/dev/null 2>&1 || { echo "ERROR: gh (GitHub CLI) not found. Install: https://cli.github.com/"; exit 1; }

# ---- Verify working tree is clean ----
if ! git diff-index --quiet HEAD -- 2>/dev/null; then
    echo "ERROR: working tree is not clean. Please commit or stash changes."
    git status --short
    exit 1
fi

# ---- Cross-compile ----
echo "=== Building $VERSION ==="

declare -A TARGETS=(
    ["windows/amd64"]="ninja-go-${VERSION}-windows-amd64.exe"
    ["linux/amd64"]="ninja-go-${VERSION}-linux-amd64"
)

BINARIES=()
for target in "${!TARGETS[@]}"; do
    goos="${target%/*}"
    goarch="${target#*/}"
    output="${TARGETS[$target]}"
    echo "  -> $output (GOOS=$goos GOARCH=$goarch)"
    GOOS="$goos" GOARCH="$goarch" go build \
        -ldflags="$LDFLAGS" \
        -o "$BUILD_DIR/$output" \
        ./ninja/
    BINARIES+=("$BUILD_DIR/$output")
done

echo ""
echo "Binaries:"
for b in "${BINARIES[@]}"; do
    printf "  %-55s %s\n" "$(basename "$b")" "$(du -h "$b" | cut -f1)"
done

# ---- Tag ----
echo ""
echo "=== Creating tag $VERSION ==="
git tag -a "$VERSION" -m "ninja-go $VERSION"
git push origin "$VERSION"

# ---- Release notes ----
REPO_URL="$(gh repo view --json url -q .url)"
RELEASE_URL="${REPO_URL}/releases/download/$VERSION"

NOTES=$(cat <<EOF
## ninja-go $VERSION

Go 语言移植的 [Ninja](https://ninja-build.org/) 构建系统。

### 下载

| 平台 | 文件 |
|------|------|
| Windows (amd64) | [ninja-go-${VERSION}-windows-amd64.exe](${RELEASE_URL}/ninja-go-${VERSION}-windows-amd64.exe) |
| Linux (amd64) | [ninja-go-${VERSION}-linux-amd64](${RELEASE_URL}/ninja-go-${VERSION}-linux-amd64) |

### 支持的子工具

browse, clean, cleandead, commands, compdb, deps, graph, inputs,
missingdeps, multi-inputs, query, recompact, restat, rules, targets, wincodepage

### 命令行选项

\`-C\`, \`-f\`, \`-j\`, \`-k\`, \`-l\`, \`-n\`, \`-d\`, \`-t\`, \`-w\`, \`-v\`, \`--version\`

### 校验和

\`\`\`
$(cd "$BUILD_DIR" && sha256sum "${TARGETS[@]}")
\`\`\`
EOF
)

# ---- Create GitHub release ----
echo ""
echo "=== Creating GitHub release ==="
gh release create "$VERSION" \
    --title "ninja-go $VERSION" \
    --notes "$NOTES" \
    "${BINARIES[@]}"

# ---- Cleanup ----
rm -rf "$BUILD_DIR"

echo ""
echo "=== Release $VERSION created ==="
echo "URL: ${REPO_URL}/releases/tag/$VERSION"
