# Icarus inventory (/v1/inventory, X31): static Go binary on scratch, like
# analyze. Build context = the repo root (compose: context ".", dockerfile
# inference/Dockerfile), because the module imports ../server (the contract,
# R3). inference/Dockerfile.dockerignore keeps the context to these two
# folders. The key and the token are read-only file mounts; events is a
# volume owned by the service user.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/go.mod ./server/
COPY server/*.go ./server/
COPY inference/go.mod inference/go.sum ./inference/
WORKDIR /src/inference
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY inference/ ./
# Tests run BEFORE ARG BUILD (X23): a new commit sha does not re-run them.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 go test -trimpath -count=1 ./...
ARG BUILD=dev
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.build=${BUILD}" -o /inventory ./cmd/inventory \
 && mkdir -p /events && chown 65534:65534 /events

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /inventory /inventory
COPY --from=build /src/inference/third_party/SAM_LICENSE.txt /licenses/SAM_LICENSE.txt
COPY --from=build --chown=65534:65534 /events /events
USER 65534:65534
# The box has 1.9 GiB: the Go heap aims to stay under this (a soft limit).
ENV GOMEMLIMIT=400MiB
VOLUME ["/events"]
EXPOSE 8087
ENTRYPOINT ["/inventory"]
CMD ["-listen", "0.0.0.0:8087", "-key-file", "/run/secrets/meta_key", \
     "-token-file", "/run/secrets/analyze_token", "-gate", "http://gate:8084", "-events", "/events"]
