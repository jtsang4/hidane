FROM node:24-alpine AS build
WORKDIR /repo
RUN corepack enable
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/server/package.json apps/server/
RUN pnpm install --frozen-lockfile
COPY apps/server ./apps/server
RUN pnpm -C apps/server build && pnpm prune --prod

FROM node:24-alpine
WORKDIR /repo/apps/server
# git/python3 for worker executions; pi ships inside node_modules (SDK + RPC CLI)
RUN apk add --no-cache git python3
# custom pi model catalog (models not yet in pi's built-in registry)
COPY deploy/pi-models.json /root/.pi/agent/models.json
COPY --from=build /repo/package.json /repo/pnpm-workspace.yaml /repo/
COPY --from=build /repo/node_modules /repo/node_modules
COPY --from=build /repo/apps/server/node_modules ./node_modules
COPY --from=build /repo/apps/server/package.json ./
COPY --from=build /repo/apps/server/dist ./dist
ENV NODE_ENV=production
ENV HIDANE_HOME=/data
VOLUME ["/data"]
EXPOSE 2718
CMD ["node", "dist/index.js", "daemon"]
