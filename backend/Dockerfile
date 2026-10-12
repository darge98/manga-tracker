# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM build AS test
ENV CGO_ENABLED=0
CMD ["go", "test", "-count=1", "./..."]

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 manga && adduser -S -D -H -u 10001 -G manga manga
WORKDIR /app
COPY --from=build --chown=manga:manga /out/server /app/server
USER manga:manga
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=6 CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/app/server"]
