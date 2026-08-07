# syntax=docker/dockerfile:1.7
FROM golang:1.24.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/meridian-gateway ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/meridian-gateway /meridian-gateway
VOLUME ["/data"]
EXPOSE 8080
ENV MERIDIAN_ADDRESS=:8080 MERIDIAN_DATABASE_PATH=/data/meridian.db MERIDIAN_BASE_URL=http://127.0.0.1:8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 CMD ["/meridian-gateway", "healthcheck"]
USER nonroot:nonroot
ENTRYPOINT ["/meridian-gateway"]
