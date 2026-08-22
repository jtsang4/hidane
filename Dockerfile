FROM node:24-alpine AS build
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY tsconfig.json ./
COPY src ./src
RUN pnpm build && pnpm prune --prod

FROM node:24-alpine
WORKDIR /app
# git/python3 for worker executions; pi ships inside node_modules (SDK + RPC CLI)
RUN apk add --no-cache git python3
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY package.json ./
ENV NODE_ENV=production
ENV HIDANE_HOME=/data
VOLUME ["/data"]
EXPOSE 2718
CMD ["node", "dist/index.js", "daemon"]
