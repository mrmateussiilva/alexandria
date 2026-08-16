FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/alexandria ./cmd/alexandria

FROM alpine:3.22
RUN apk add --no-cache poppler-utils
RUN addgroup -S alexandria && adduser -S -G alexandria alexandria
RUN mkdir -p /config /books && chown -R alexandria:alexandria /config
COPY --from=go-build /out/alexandria /usr/local/bin/alexandria
USER alexandria
EXPOSE 8080
ENV ALEXANDRIA_SERVER_ADDRESS=0.0.0.0
ENV ALEXANDRIA_SERVER_PORT=8080
ENV ALEXANDRIA_DATABASE_PATH=/config/alexandria.db
ENV ALEXANDRIA_CACHE_PATH=/config/cache
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:$${ALEXANDRIA_SERVER_PORT:-8080}/api/health >/dev/null || exit 1
ENTRYPOINT ["alexandria"]
