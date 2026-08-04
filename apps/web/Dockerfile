# Build the SvelteKit app with bun (workspace root owns bun.lock).
FROM oven/bun:1 AS build
WORKDIR /app
COPY package.json bun.lock ./
COPY apps/web/package.json ./apps/web/
RUN bun install --frozen-lockfile
COPY apps/web ./apps/web
WORKDIR /app/apps/web
RUN bun run build

# adapter-node output is self-contained: no node_modules at runtime.
# server.js adds the /api, /auth, and /health proxy to the Go API.
FROM node:22-alpine
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build --chown=node:node /app/apps/web/build ./build
COPY --chown=node:node apps/web/server.js ./server.js
USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
	CMD node -e "fetch('http://127.0.0.1:3000/health').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"
CMD ["node", "server.js"]
