FROM node:24-alpine AS build
WORKDIR /repo
ENV CI=true
RUN corepack enable
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/server/package.json apps/server/
COPY apps/web/package.json apps/web/
RUN pnpm install --frozen-lockfile
COPY apps/server ./apps/server
COPY apps/web ./apps/web
RUN pnpm -C apps/server build && pnpm -C apps/web build \
  && rm -rf node_modules apps/server/node_modules apps/web/node_modules \
  && pnpm install --prod --frozen-lockfile

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
# pi extensions are loaded from source at runtime (rpcWorker resolves ../../extensions)
COPY --from=build /repo/apps/server/extensions ./extensions
COPY --from=build /repo/apps/web/dist /repo/apps/web/dist
ENV NODE_ENV=production
ENV HIDANE_HOME=/data
VOLUME ["/data"]
EXPOSE 2718
CMD ["node", "dist/index.js", "daemon"]
