#!/bin/bash

# Exit immediately if a command exits with a non-zero status
set -e

echo "🔧 Setting up tmux..."
# Create a symbolic link for the tmux configuration
git clone https://github.com/tmux-plugins/tpm ~/.tmux/plugins/tpm
ln -sf /workspaces/memax/.devcontainer/.tmux.conf /home/node/.tmux.conf

if tmux ls >/dev/null 2>&1; then
  echo "🔄 Reloading tmux configuration..."
  tmux source-file /workspaces/memax/.devcontainer/.tmux.conf
fi

echo "🔧 Fixing permissions for agent CLIs and XDG config..."
sudo chown -R node:node /home/node/.claude /home/node/.codex /home/node/.gemini /home/node/.copilot /home/node/.config

echo "🔐 Routing git credentials through gh (persisted across rebuilds)..."
# Once `gh auth login` has run, gh stores the token in ~/.config/gh/ which
# is now on a named volume. Tell git to use gh's credential helper so every
# rebuild picks up the stored token automatically instead of prompting or
# relying on the VSCode devcontainer credential socket (which goes stale
# whenever the extension host restarts).
if command -v gh >/dev/null 2>&1; then
  git config --global --replace-all credential.https://github.com.helper "!gh auth git-credential"
  git config --global --replace-all credential.https://gist.github.com.helper "!gh auth git-credential"
fi

echo "🔗 Persisting agent CLI session metadata across rebuilds..."
# ~/.claude and ~/.codex are named volumes, but Claude Code and Codex also
# write a sibling JSON file at $HOME which lives in the ephemeral container
# filesystem and is wiped on every rebuild. Symlink each one into the
# persisted volume so authentication survives.
for cli in claude codex; do
  target="/home/node/.${cli}/session.json"
  link="/home/node/.${cli}.json"
  mkdir -p "$(dirname "$target")"
  if [ -f "$link" ] && [ ! -L "$link" ]; then
    mv "$link" "$target"
  fi
  [ -f "$target" ] || touch "$target"
  ln -sfn "$target" "$link"
done

echo "🐋 Restoring optional Orca remote server (opt-in, per-developer)..."
# No-op unless ~/.config/orca-app/.enabled exists. That path is a named volume,
# so this only fires for developers who ran `setup-orca.sh enable` in their own
# container; everyone else pays nothing.
bash .devcontainer/setup-orca.sh start || echo "⚠️  Orca start failed; continuing devcontainer setup."

echo "📦 Installing dependencies with pnpm..."
pnpm install

echo "📚 Preparing public SDK/CLI checkout..."
bash .devcontainer/setup-public-ref.sh || echo "⚠️  Public SDK/CLI setup failed; continuing devcontainer setup."

echo "📚 Preparing public Agent SDK checkout..."
bash .devcontainer/setup-agent-sdk-ref.sh || echo "⚠️  Agent SDK setup failed; continuing devcontainer setup."

echo "🚀 Downloading Go modules for the server package..."
cd packages/server
go mod download

echo "🔐 Setting up Doppler CLI..."
cd ../..
doppler setup --no-interactive

echo "🔨 Pre-building the project (ignoring failures)..."
pnpm run build || true

echo "✅ Post-create script completed successfully!"

echo "🛠️ Installing agent CLI wrapper commands..."
mkdir -p /home/node/.local/bin

install_wrapper() {
  local name="$1"
  local command="$2"

  cat >"/home/node/.local/bin/${name}" <<EOF
#!/bin/bash
set -e
exec ${command} "\$@"
EOF
  chmod +x "/home/node/.local/bin/${name}"
}

install_wrapper "claude-auto" "claude --dangerously-skip-permissions"
install_wrapper "codex-auto" "codex --dangerously-bypass-approvals-and-sandbox"
install_wrapper "gemini-auto" "gemini --yolo"
install_wrapper "copilot-auto" "copilot --yolo"
install_wrapper "copilot-full" "copilot --autopilot --yolo"
