#!/bin/bash
set -e

VERSION="1.0.0"
TAG="v${VERSION}"
REPO="Syfra3/ancora"
TOKEN=$(gh auth token)

# Create release via API
echo "Creating release ${TAG}..."

RELEASE_DATA=$(cat <<JSON
{
  "tag_name": "${TAG}",
  "name": "v${VERSION}",
  "body": "## Ancora v${VERSION} - Initial Release\n\nAncora is a persistent memory system for AI coding agents.\n\n### Features\n- Hybrid Search (FTS5 + semantic embeddings)\n- MCP Server Integration\n- TUI Interface\n- Project & Personal Scopes\n\n### Installation\n\`\`\`bash\nbrew tap Syfra3/tap\nbrew install ancora\n\`\`\`\n\nSee full release notes at https://github.com/Syfra3/ancora/releases/tag/${TAG}",
  "draft": false,
  "prerelease": false
}
JSON
)

RESPONSE=$(curl -s -X POST \
  -H "Authorization: token ${TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/${REPO}/releases" \
  -d "$RELEASE_DATA")

RELEASE_ID=$(echo "$RESPONSE" | grep -o '"id": [0-9]*' | head -1 | cut -d' ' -f2)
UPLOAD_URL=$(echo "$RESPONSE" | grep -o '"upload_url": "[^"]*"' | cut -d'"' -f4 | sed 's/{?name,label}//')

if [ -z "$RELEASE_ID" ]; then
  echo "Error creating release:"
  echo "$RESPONSE"
  exit 1
fi

echo "✓ Release created (ID: ${RELEASE_ID})"

# Upload assets
for tarball in dist/ancora-${VERSION}-*.tar.gz; do
  filename=$(basename "$tarball")
  echo "Uploading ${filename}..."
  
  curl -s -X POST \
    -H "Authorization: token ${TOKEN}" \
    -H "Content-Type: application/gzip" \
    -H "Accept: application/vnd.github+json" \
    "${UPLOAD_URL}?name=${filename}" \
    --data-binary "@${tarball}" > /dev/null
  
  echo "✓ Uploaded ${filename}"
done

echo ""
echo "✓ Release complete: https://github.com/${REPO}/releases/tag/${TAG}"
