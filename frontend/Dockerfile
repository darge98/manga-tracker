FROM node:24.21.0-bookworm-slim AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY angular.json tsconfig.json tsconfig.app.json ./
COPY src ./src
COPY public ./public
RUN npm run build

FROM nginxinc/nginx-unprivileged:stable-alpine AS runtime
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY security-headers.conf /etc/nginx/snippets/security-headers.conf
COPY --from=build /app/dist/frontend/browser /usr/share/nginx/html
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
