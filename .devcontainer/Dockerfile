FROM mcr.microsoft.com/devcontainers/typescript-node:22

# ========= Go 1.25 =========
ARG GO_VERSION=1.25.0
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/home/node/go"
ENV PATH="${GOPATH}/bin:${PATH}"
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# ========= pnpm =========
RUN corepack enable && corepack prepare pnpm@10.33.0 --activate
ENV PNPM_HOME="/home/node/.local/share/pnpm"
ENV PATH="${PNPM_HOME}:${PATH}"
RUN mkdir -p "${PNPM_HOME}" && chown -R node:node "${PNPM_HOME}"

# ========= Tools & Database clients (psql, redis-cli) =========
RUN apt-get update && apt-get install -y --no-install-recommends tmux postgresql-client redis-tools cron \
    && rm -rf /var/lib/apt/lists/*

# ========= golang-migrate CLI =========
RUN curl -fsSL https://github.com/golang-migrate/migrate/releases/download/v4.19.1/migrate.linux-amd64.tar.gz | tar -xz -C /usr/local/bin

# ========= GitHub CLI =========
RUN (type -p wget >/dev/null || (apt-get update && apt-get install -y wget)) \
    && mkdir -p -m 755 /etc/apt/keyrings \
    && wget -nv -O /etc/apt/keyrings/githubcli-archive-keyring.gpg https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# ========= Rust =========
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | bash -s -- -y
ENV PATH="/root/.cargo/bin:/home/node/.cargo/bin:${PATH}"
# Also install for the non-root user (devcontainer runs as node)
USER node
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | bash -s -- -y
USER root

# ========= Doppler CLI =========
RUN apt-get update && apt-get install -y apt-transport-https ca-certificates curl gnupg \
    && curl -sLf --retry 3 --tlsv1.2 --proto "=https" 'https://packages.doppler.com/public/cli/gpg.DE2A7741A397C129.key' | gpg --dearmor -o /usr/share/keyrings/doppler-archive-keyring.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/doppler-archive-keyring.gpg] https://packages.doppler.com/public/cli/deb/debian any-version main" | tee /etc/apt/sources.list.d/doppler-cli.list \
    && apt-get update && apt-get install doppler

# ========= Fly.io CLI =========
RUN curl -L https://fly.io/install.sh | sh -s -- --non-interactive --setup-path
USER node
RUN curl -L https://fly.io/install.sh | sh -s -- --non-interactive --setup-path
USER root

# ========= Claude Code =========
ENV PATH="/root/.local/bin:/home/node/.local/bin:${PATH}"

# Install for node user
RUN mkdir -p /home/node/.local/bin /home/node/.claude && \
    chown -R node:node /home/node/.local /home/node/.claude
USER node
RUN curl -fsSL https://claude.ai/install.sh | bash
# Install for root user
USER root
RUN ln -sf /home/node/.local/bin/claude /usr/local/bin/claude

# ========= Codex CLI =========
RUN mkdir -p /home/node/.codex && \
    chown -R node:node home/node/.codex
RUN npm install -g @openai/codex

# ========= Gemini CLI =========
RUN mkdir -p /home/node/.gemini && \
    chown -R node:node home/node/.gemini
RUN npm install -g @google/gemini-cli

# ========= GitHub Copilot CLI =========
RUN mkdir -p /home/node/.copilot && \
    chown -R node:node home/node/.copilot
RUN curl -fsSL https://gh.io/copilot-install | bash || true
