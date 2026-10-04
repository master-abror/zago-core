# Build context = ROOT repo. Workspaces npm: semua package.json harus disalin sebelum `npm ci`.
ARG NODE_VERSION=24
FROM node:${NODE_VERSION}-alpine AS base
WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/web/package.json apps/web/
COPY packages/ts-sdk/package.json packages/ts-sdk/
COPY packages/ui/package.json packages/ui/
RUN npm ci

FROM base AS dev
CMD ["npm", "run", "dev", "-w", "apps/web", "--", "--host"]

FROM base AS build
COPY apps ./apps
COPY packages ./packages
COPY modules ./modules
RUN npm run build -w apps/web

FROM nginx:alpine AS production
COPY --from=build /app/apps/web/dist /usr/share/nginx/html
COPY deploy/nginx/web.conf /etc/nginx/conf.d/default.conf
