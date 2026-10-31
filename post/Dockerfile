# Icarus post services (X35): touchup, rescue, fanout, voice. Static Go binaries on
# scratch, like inference. Build context = the repo root (compose: context ".",
# dockerfile post/Dockerfile), because the module imports ../server, ../listings and ../inference.
# post/Dockerfile.dockerignore keeps the context to these folders. One
# image holds every binary in post/cmd; compose picks one per service with
# "entrypoint". The key and the token are read-only file mounts; events and
# the stores are volumes owned by the service user.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/go.mod ./server/
COPY server/*.go ./server/
# X35d: the voice service imports the listing contract (listings/: limits + Validate).
COPY listings/go.mod ./listings/
COPY listings/*.go ./listings/
# X49f: the ZIP-center table that listings/place.go embeds (X49e)
COPY listings/zcta.csv ./listings/
# X48o: the touch-up service runs the auto touch-up of the inference module
# (its Go files only; golang.org/x/image, BSD-3-Clause).
COPY inference/go.mod inference/go.sum ./inference/
COPY inference/*.go ./inference/
COPY post/go.mod post/go.sum ./post/
WORKDIR /src/post
# X35d: the voice service uses github.com/go-audio/wav (Apache-2.0).
RUN --mount=type=cache,target=/go/pkg/mod go mod download
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
COPY --from=build /src/post/third_party/GO_AUDIO_LICENSE.txt /licenses/GO_AUDIO_LICENSE.txt
# X48o: the touch-up calls SAM 3.1 (the SAM License, as in the inference image).
COPY inference/third_party/SAM_LICENSE.txt /licenses/SAM_LICENSE.txt
COPY --from=build --chown=65534:65534 /data /data
COPY --from=build --chown=65534:65534 /events /events
USER 65534:65534
ENV GOMEMLIMIT=200MiB
VOLUME ["/data", "/events"]
ENTRYPOINT ["/touchup"]
CMD ["-listen", "0.0.0.0:8088", "-key-file", "/run/secrets/meta_key", \
     "-auth", "http://auth:8081", "-item-photos", "/item-photos", \
     "-dir", "/data", "-events", "/events"]
