# V3RDE telemetry: the sidecar and the ledger, static Go on scratch, user
# 65534. One image, three binaries: compose picks /sidecar or /ledger as the
# entrypoint; /edgecloud (X44d) prints the edge vs cloud report of the ledger
# (docker run --rm --entrypoint /edgecloud -v <ledger>:/ledger-data:ro ...).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd ./cmd
# Go cache mounts (https://docs.docker.com/build/cache/optimize/#use-cache-mounts).
# The tests run before ARG BUILD, so a new commit does not run them again when
# the code is the same (https://docs.docker.com/build/cache/invalidation/).
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go test -trimpath -count=1 ./...
ARG BUILD=dev
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.build=${BUILD}" -o /sidecar ./cmd/sidecar \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.build=${BUILD}" -o /ledger ./cmd/ledger \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /edgecloud ./cmd/edgecloud \
 && mkdir -p /spool /ledgerdir /sock && chown 65534:65534 /spool /ledgerdir /sock

FROM scratch
COPY --from=build /sidecar /sidecar
COPY --from=build /ledger /ledger
COPY --from=build /edgecloud /edgecloud
COPY --from=build --chown=65534:65534 /spool /spool
COPY --from=build --chown=65534:65534 /ledgerdir /ledger-data
# US: the socket folder, owned by the service user, so a new v3rde_sock volume gets that owner (as /events)
COPY --from=build --chown=65534:65534 /sock /sock
USER 65534:65534
ENTRYPOINT ["/sidecar"]
