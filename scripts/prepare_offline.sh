#!/bin/bash

# ==========================================
#  ALEUTIAN OFFLINE PREP SCRIPT (v2)
# ==========================================

# 1. Setup Directories
# We use -p to ensure we don't error if they already exist
TARGET_DIR="/Volumes/ai_models/Aleutian_Reference"
mkdir -p "$TARGET_DIR/repos"
mkdir -p "$TARGET_DIR/docs"
# We assume you manually placed your docsets here:
DOCSET_DIR="$TARGET_DIR/docsets"

# 2. List of Repos to Clone
REPOS=(
    "https://github.com/weaviate/weaviate.git"
    "https://github.com/langchain-ai/langchain.git"
    "https://github.com/langchain-ai/local-deep-researcher.git"
    "https://github.com/run-llama/llama_index.git"
    "https://github.com/microsoft/graphrag.git"
    "https://github.com/explodinggradients/ragas.git"
    "https://github.com/langchain-ai/langgraph.git"
    "https://github.com/langchain-ai/docs.git"
    "https://github.com/spf13/cobra.git"
    "https://github.com/gin-gonic/gin.git"
    "https://github.com/open-policy-agent/opa.git"
    "https://github.com/influxdata/influxdb.git"
)

echo "--- 📦 Cloning Repositories ---"
for repo in "${REPOS[@]}"; do
    cd "$TARGET_DIR/repos"
    dir_name=$(basename "$repo" .git)

    if [ ! -d "$dir_name" ]; then
        echo "   ⬇️  Cloning $dir_name..."
        # Depth 1 is crucial for speed/space when going offline
        git clone --depth 1 "$repo"
    else
        echo "   🔄 Updating $dir_name..."
        cd "$dir_name"
        git pull
    fi
done

# 3. The "Wget" Scraper for Documentation sites
echo "--- 📄 Scraping Documentation Sites ---"
# Check if wget is installed
if ! command -v wget &> /dev/null; then
    echo "❌ Error: 'wget' is not installed. Run 'brew install wget' first."
    exit 1
fi

cd "$TARGET_DIR/docs"
DOC_SITES=(
    "https://gams.weaviate.io/"
    "https://docs.llamaindex.ai/en/stable/"
    "https://microsoft.github.io/graphrag/"
)

for site in "${DOC_SITES[@]}"; do
    echo "   🌐 Mirroring $site..."
    wget --mirror --convert-links --adjust-extension --page-requisites --no-parent -q "$site"
done

# 4. The "Deep Unpack" for Docsets (THE FIX)
echo "--- 📦 Processing Docsets ---"
# ==========================================
#  ALEUTIAN DOCSET INGESTOR (v2 - "The Finder")
# ==========================================

# 1. Configuration
# Point this to the root where your docsets live
DOCSET_ROOT="/Volumes/ai_models/Aleutian_Reference/docsets"
# Point this to your Aleutian binary
ALEUTIAN_BIN="/Users/jin/GolandProjects/AleutianLocal/aleutian"

# 2. The Loop
echo "🚀 Starting Intelligent Docset Ingestion..."
echo "📂 Scanning: $DOCSET_ROOT"

# Find every directory ending in .docset
find "$DOCSET_ROOT" -name "*.docset" -type d | while read docset_path; do

    # Get a clean name for the dataspace (e.g., "Matplotlib.docset" -> "matplotlib")
    # We use the basename of the *outermost* docset folder
    name=$(basename "$docset_path" .docset | tr '[:upper:]' '[:lower:]')

    echo "---------------------------------------------------"
    echo "🔍 Processing: $name"

    # 3. THE FIX: Find the *deepest* 'index.html' file inside this docset
    # We search inside the docset folder for "index.html"
    # We take the first result (head -n 1) and get its directory (dirname)
    target_dir=$(find "$docset_path" -name "index.html" -print0 | xargs -0 dirname 2>/dev/null | head -n 1)

    if [ -n "$target_dir" ]; then
        echo "   ✅ Found content at: $target_dir"

        # 4. Ingest that specific directory
        echo "   ⏳ Ingesting into data-space: '$name'..."

        $ALEUTIAN_BIN populate vectordb "$target_dir" \
            --force \
            --data-space "$name"

        echo "   🎉 Done!"
    else
        echo "   ⚠️  SKIPPING: Could not find an 'index.html' inside $docset_path"
        echo "      (The unpack might have failed, or it's a different format)"
    fi
done

echo "---------------------------------------------------"
echo "✅ All Docsets Processed."

echo "✅ Offline Prep Complete!"