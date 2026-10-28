# Icarus post services (X35): touchup, rescue, fanout. Static Go binaries on
# scratch, like inference. Build context = the repo root (compose: context ".",
# dockerfile post/Dockerfile), because the module imports ../server.
# post/Dockerfile.dockerignore keeps the context to these two folders. One
# image holds every binary in post/cmd; compose picks one per service with
# "entrypoint". The key and the token are read-only file mounts; events and
# the stores are volumes owned by the service user.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/go.mod ./server/
COPY server/*.go ./server/
COPY post/go.mod ./post/
WORKDIR /src/post
COPY post/ ./
# Tests run BEFORE ARG BUILD (X23): a new commit sha does not re-run them.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 go test -trimpath -count=1 ./...
ARG BUILD=dev
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod mkdir -p /out \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.build=${BUILD}" -o /out/ ./cmd/... \
 && mkdir -p /data /events && chown 65534:65534 /data /events

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ /
COPY --from=build --chown=65534:65534 /data /data
COPY --from=build --chown=65534:65534 /events /events
USER 65534:65534
ENV GOMEMLIMIT=200MiB
VOLUME ["/data", "/events"]
ENTRYPOINT ["/touchup"]
CMD ["-listen", "0.0.0.0:8088", "-key-file", "/run/secrets/meta_key", \
     "-token-file", "/run/secrets/analyze_token", "-dir", "/data", "-events", "/events"]
