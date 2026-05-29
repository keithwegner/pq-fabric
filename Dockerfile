FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/validator ./cmd/validator
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/relay ./cmd/relay
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/demo ./cmd/demo
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fault-demo ./cmd/fault-demo
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/routing-demo ./cmd/routing-demo
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bundle-demo ./cmd/bundle-demo
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/anchor-demo ./cmd/anchor-demo
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/deployment-evidence ./cmd/deployment-evidence
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pilot-deploy-check ./cmd/pilot-deploy-check
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pilot-bootstrap ./cmd/pilot-bootstrap
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sqlite-restore-check ./cmd/sqlite-restore-check
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/e2e-demo ./cmd/e2e-demo
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/e2e-evidence ./cmd/e2e-evidence
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pqfabric ./cmd/pqfabric
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM debian:bookworm-slim

WORKDIR /app
RUN groupadd -r pqfabric && useradd -r -g pqfabric -d /app -s /usr/sbin/nologin pqfabric
COPY --from=build /out/ /app/
RUN mkdir -p /data /app/tmp && chown -R pqfabric:pqfabric /data /app/tmp
USER pqfabric
EXPOSE 8080
ENTRYPOINT ["/app/validator"]
