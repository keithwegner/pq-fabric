FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build

WORKDIR /src
ARG TARGETOS=linux
ARG TARGETARCH
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN target_arch="${TARGETARCH:-$(go env GOARCH)}"; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/validator ./cmd/validator; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/relay ./cmd/relay; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/demo ./cmd/demo; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/fault-demo ./cmd/fault-demo; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/routing-demo ./cmd/routing-demo; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/bundle-demo ./cmd/bundle-demo; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/anchor-demo ./cmd/anchor-demo; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/deployment-evidence ./cmd/deployment-evidence; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/pilot-deploy-check ./cmd/pilot-deploy-check; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/pilot-bootstrap ./cmd/pilot-bootstrap; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/sqlite-restore-check ./cmd/sqlite-restore-check; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/e2e-demo ./cmd/e2e-demo; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/e2e-evidence ./cmd/e2e-evidence; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/pqfabric ./cmd/pqfabric; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${target_arch}" go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM debian:bookworm-slim

WORKDIR /app
RUN apt-get update \
  && apt-get upgrade -y \
  && rm -rf /var/lib/apt/lists/*
RUN groupadd -r pqfabric && useradd -r -g pqfabric -d /app -s /usr/sbin/nologin pqfabric
COPY --from=build /out/ /app/
RUN mkdir -p /data /app/tmp && chown -R pqfabric:pqfabric /data /app/tmp
USER pqfabric
EXPOSE 8080
ENTRYPOINT ["/app/validator"]
