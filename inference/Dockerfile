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
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /calibrate ./cmd/calibrate \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /learn ./cmd/learn \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /touchupcompare ./cmd/touchupcompare \
 && mkdir -p /events /sock && chown 65534:65534 /events /sock

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /inventory /inventory
# X44g: the labels report (docker run --rm --entrypoint /calibrate -v <analyze events>:/events:ro ...)
COPY --from=build /calibrate /calibrate
# LR3: the daily learn report (tools/learn_round.sh: docker run --rm --network none --entrypoint /learn ...)
COPY --from=build /learn /learn
# X48f: the touch-up v2 comparison (docker run --rm --entrypoint /touchupcompare -v <photos>:/photos:ro ...)
COPY --from=build /touchupcompare /touchupcompare
COPY --from=build /src/inference/third_party/SAM_LICENSE.txt /licenses/SAM_LICENSE.txt
COPY --from=build --chown=65534:65534 /events /events
# US: the socket folder, owned by the service user, so a new v3rde_sock volume gets that owner (as /events)
COPY --from=build --chown=65534:65534 /sock /sock
USER 65534:65534
# The box has 1.9 GiB: the Go heap aims to stay under this (a soft limit).
ENV GOMEMLIMIT=400MiB
VOLUME ["/events"]
EXPOSE 8087
ENTRYPOINT ["/inventory"]
CMD ["-listen", "0.0.0.0:8087", "-key-file", "/run/secrets/meta_key", \
     "-token-file", "/run/secrets/analyze_token", "-gate", "http://gate:8084", "-events", "/events"]
