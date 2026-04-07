#!/bin/bash
# Build ancora v1.0.0 for multiple platforms

VERSION="1.0.0"
LDFLAGS="-s -w -X main.version=${VERSION}"

# Platforms to build
declare -a platforms=(
    "linux:amd64"
    "linux:arm64"
    "darwin:amd64"
    "darwin:arm64"
)

mkdir -p dist

for platform in "${platforms[@]}"; do
    IFS=':' read -r -a parts <<< "$platform"
    GOOS="${parts[0]}"
    GOARCH="${parts[1]}"
    
    output="dist/ancora-${VERSION}-${GOOS}-${GOARCH}"
    if [ "$GOOS" = "windows" ]; then
        output="${output}.exe"
    fi
    
    echo "Building ${GOOS}/${GOARCH}..."
    GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$output" ./cmd/ancora
    
    # Create tarball
    tar_name="ancora-${VERSION}-${GOOS}-${GOARCH}.tar.gz"
    tar -czf "dist/${tar_name}" -C dist "$(basename $output)"
    
    echo "✓ Created dist/${tar_name}"
done

echo ""
echo "All builds complete in dist/"
ls -lh dist/*.tar.gz
