#!/bin/bash
# Build the Next.js frontend as a static export for serving from Go.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

echo "==> Installing dependencies..."
pnpm install

echo "==> Running next build..."
SKIP_ENV_VALIDATION=1 npx next build

echo "==> Static export complete! Output in: out/"
