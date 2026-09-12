#!/usr/bin/env bash
# Optional: run an Orca Remote Server inside this devcontainer, reachable from
# your laptop and phone over Tailscale.
#
#   https://onorca.dev/docs/remote-servers
#
# OPT-IN, PER DEVELOPER. This script is a no-op unless the marker file
# ~/.config/orca-app/.enabled exists in your own container. Enable it once:
#
#   bash .devcontainer/setup-orca.sh enable
#
# after which post-create and post-start bring it back up on every rebuild.
# Disable with `bash .devcontainer/setup-orca.sh disable`.
#
# ~/.config is a named Docker volume (see docker-compose.yml), so the Orca
# profile, the extracted app, and the Tailscale node key all survive rebuilds.
# Everything on the container filesystem (apt libs, /usr/local/bin) does not,
# and is reinstalled here.
#
# Usage: bash .devcontainer/setup-orca.sh [start|stop|status|enable|disable]

set -euo pipefail

ORCA_HOME="/home/node/.config/orca-app"
ORCA_APP="$ORCA_HOME/squashfs-root/orca-ide"
ORCA_APPIMAGE="$ORCA_HOME/orca-linux.AppImage"
ORCA_MARKER="$ORCA_HOME/.enabled"
ORCA_PORT="${ORCA_PORT:-6768}"
TS_HOME="/home/node/.config/tailscale"
TS_SOCK="/tmp/tailscaled.sock"
TS_HOSTNAME="${TS_HOSTNAME:-memax-coder-${CODER_WORKSPACE_OWNER_NAME:-dev}}"
LOG_DIR="$ORCA_HOME/logs"

log() { echo "[orca] $*"; }

ensure_apt_deps() {
  # Electron/Chromium runtime libraries. Wiped on every devcontainer rebuild.
  if ldconfig -p | grep -q libgtk-3.so.0 && command -v xvfb-run >/dev/null 2>&1; then
    return
  fi
  log "installing Electron runtime libraries (apt)..."
  sudo apt-get update -qq
  sudo apt-get install -y -qq curl file jq xvfb ca-certificates git \
    libgtk-3-0t64 libnss3 libatk1.0-0t64 libatk-bridge2.0-0t64 libgbm1 \
    libasound2t64 libxtst6 libcups2t64 libdrm2 libxkbcommon0 libpango-1.0-0 \
    libcairo2 libatspi2.0-0t64 libxcomposite1 libxdamage1 libxfixes3 \
    libxrandr2 libxrender1 libx11-xcb1 libxcb-dri3-0 libxss1
}

ensure_orca() {
  [ -x "$ORCA_APP" ] && return
  if [ ! -f "$ORCA_APPIMAGE" ]; then
    log "downloading Orca AppImage (~195MB)..."
    curl -fL --retry 3 -o "$ORCA_APPIMAGE.tmp" \
      https://github.com/stablyai/orca/releases/latest/download/orca-linux.AppImage
    mv "$ORCA_APPIMAGE.tmp" "$ORCA_APPIMAGE"
    chmod +x "$ORCA_APPIMAGE"
  fi
  # There is no /dev/fuse in this container, so extract rather than mount.
  log "extracting AppImage..."
  (cd "$ORCA_HOME" && "$ORCA_APPIMAGE" --appimage-extract >/dev/null)
}

ensure_tailscale() {
  if [ ! -x "$TS_HOME/tailscaled" ]; then
    log "downloading Tailscale..."
    local asset
    asset="$(curl -fsS 'https://pkgs.tailscale.com/stable/?mode=json' | jq -r '.Tarballs.amd64')"
    curl -fsSL -o /tmp/ts.tgz "https://pkgs.tailscale.com/stable/${asset}"
    tar xzf /tmp/ts.tgz -C /tmp
    cp "/tmp/${asset%.tgz}/tailscaled" "/tmp/${asset%.tgz}/tailscale" "$TS_HOME/"
  fi
  # Restore the persisted binaries into the ephemeral container filesystem.
  [ -x /usr/local/bin/tailscaled ] || sudo install -m755 "$TS_HOME/tailscaled" /usr/local/bin/tailscaled
  [ -x /usr/local/bin/tailscale ] || sudo install -m755 "$TS_HOME/tailscale" /usr/local/bin/tailscale

  if ! pgrep -x tailscaled >/dev/null 2>&1; then
    # No /dev/net/tun in this container, so run the userspace network stack.
    log "starting tailscaled (userspace networking)..."
    sudo setsid /usr/local/bin/tailscaled \
      --tun=userspace-networking \
      --socket="$TS_SOCK" \
      --state="$TS_HOME/tailscaled.state" \
      --statedir="$TS_HOME" \
      --socks5-server=localhost:1055 \
      >"$LOG_DIR/tailscaled.log" 2>&1 </dev/null &
    sleep 4
  fi

  # The node key is persisted, so this is silent after the first authentication.
  sudo /usr/local/bin/tailscale --socket="$TS_SOCK" up \
    --hostname="$TS_HOSTNAME" --accept-dns=false >"$LOG_DIR/tailscale-up.log" 2>&1 || true

  if ! sudo /usr/local/bin/tailscale --socket="$TS_SOCK" ip -4 >/dev/null 2>&1; then
    log "Tailscale needs authentication — open the link below, then re-run this script:"
    cat "$LOG_DIR/tailscale-up.log"
    exit 1
  fi
}

start_orca() {
  if pgrep -f 'orca-ide .*[s]erve' >/dev/null 2>&1; then
    log "already running"
    return
  fi
  local addr
  addr="$(sudo /usr/local/bin/tailscale --socket="$TS_SOCK" ip -4 | head -1)"
  log "starting orca serve on ${addr}:${ORCA_PORT}"
  # --no-sandbox: the container's seccomp profile blocks Chromium's namespace sandbox.
  setsid env LIBGL_ALWAYS_SOFTWARE=1 ELECTRON_DISABLE_SANDBOX=1 \
    "$ORCA_APP" --no-sandbox serve --port "$ORCA_PORT" --pairing-address "$addr" \
    >"$LOG_DIR/orca-serve.log" 2>&1 </dev/null &
  sleep 12
  grep -E 'Orca server ready|Advertised endpoint|Web client URL|Pairing URL' "$LOG_DIR/orca-serve.log" || {
    log "did not come up — see $LOG_DIR/orca-serve.log"
    exit 1
  }
}

mkdir -p "$LOG_DIR"

case "${1:-start}" in
  enable)
    touch "$ORCA_MARKER"
    log "enabled for this container"
    exec "$0" start
    ;;
  disable)
    rm -f "$ORCA_MARKER"
    exec "$0" stop
    ;;
  start)
    [ -f "$ORCA_MARKER" ] || exit 0
    ensure_apt_deps
    ensure_orca
    ensure_tailscale
    start_orca
    ;;
  stop)
    pkill -f 'orca-ide .*[s]erve' || true
    sudo pkill -x tailscaled || true
    log "stopped"
    ;;
  status)
    echo "--- tailscale ---"
    sudo /usr/local/bin/tailscale --socket="$TS_SOCK" status 2>&1 | head -10 || true
    echo "--- orca ---"
    if pgrep -f 'orca-ide .*[s]erve' >/dev/null 2>&1; then echo "running"; else echo "stopped"; fi
    grep -E 'Web client URL|Pairing URL' "$LOG_DIR/orca-serve.log" 2>/dev/null | tail -2 || true
    ;;
  *)
    echo "usage: $0 [start|stop|status|enable|disable]" >&2
    exit 1
    ;;
esac
