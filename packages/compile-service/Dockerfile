# Memax compile service: @memaxlabs/compiler over HTTP, as the Node server.
#
# Memax runs it as a Cloudflare Worker (wrangler.jsonc); this image is for
# self-hosting it, next to the API and worker. Set COMPILE_SERVICE_TOKEN on
# it and on them unless only they can reach its port.
#
# The build context is the REPOSITORY ROOT, because the service depends on
# the workspace package packages/compiler:
#
#   docker build -f packages/compile-service/Dockerfile .
#
# Dockerfile.dockerignore (next to this file) keeps node_modules, .git and
# every other package out of the context.

FROM node:24-alpine AS build
WORKDIR /repo
# pnpm comes from the root package.json's packageManager field.
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.json ./
COPY packages/compiler/package.json packages/compiler/
COPY packages/compile-service/package.json packages/compile-service/
RUN pnpm install --frozen-lockfile --filter @memaxlabs/compile-service...
COPY packages/compiler packages/compiler
COPY packages/compile-service packages/compile-service
RUN pnpm --filter @memaxlabs/compiler build \
 && pnpm --filter @memaxlabs/compile-service build \
 && pnpm --filter @memaxlabs/compile-service deploy --prod --legacy /out

FROM node:24-alpine
ENV NODE_ENV=production PORT=8080 HOST=::
WORKDIR /app
COPY --from=build /out/package.json ./
COPY --from=build /out/dist ./dist
COPY --from=build /out/node_modules ./node_modules
USER node
EXPOSE 8080
# A health check for plain Docker.
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/health > /dev/null || exit 1
CMD ["node", "dist/main.js"]
