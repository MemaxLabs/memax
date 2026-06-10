#!/bin/bash
#
# Clones (or pulls) the public Memax Agent SDK Go module into .refs/.
# This is a READ-ONLY reference checkout — Go code in packages/server/
# imports it via `go get github.com/MemaxLabs/memax-go-agent-sdk`, NOT
# from this checkout. The local checkout exists so agents working in
# this monorepo can read the SDK source, run its tests, and stage
# cross-repo changes (mirroring how .refs/memax/ works for the public
# SDK + CLI).
#
# Differences from setup-public-ref.sh: no pnpm install, no build, no
# symlink into packages/web/node_modules. Just `git clone` (or `git
# pull` if already cloned). Faster, simpler, idempotent.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_url="${MEMAX_AGENT_SDK_URL:-https://github.com/MemaxLabs/memax-go-agent-sdk.git}"
agent_dir="${MEMAX_AGENT_SDK_DIR:-$repo_root/.refs/memax-go-agent-sdk}"

echo "📚 Setting up Memax Agent SDK reference checkout..."
mkdir -p "$(dirname "$agent_dir")"

if [ -e "$agent_dir" ] && [ ! -d "$agent_dir/.git" ]; then
  echo "⚠️  $agent_dir exists but is not a git checkout; skipping agent SDK setup."
  exit 0
fi

if [ ! -d "$agent_dir/.git" ]; then
  git clone "$agent_url" "$agent_dir"
else
  current_branch="$(git -C "$agent_dir" branch --show-current || true)"
  if [ -n "$(git -C "$agent_dir" status --porcelain)" ]; then
    echo "ℹ️  Agent SDK checkout has local changes; leaving it untouched."
  elif [ "$current_branch" = "main" ]; then
    git -C "$agent_dir" pull --ff-only
  else
    echo "ℹ️  Agent SDK checkout is on '$current_branch'; fetching but not switching branches."
    git -C "$agent_dir" fetch origin
  fi
fi

echo "✅ Memax Agent SDK reference checkout ready at $agent_dir"
