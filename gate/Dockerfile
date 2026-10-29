# V3RDE food gate: Go + LiteRT + libjpeg-turbo (cgo), CPU, one interpreter x
# one thread. The runtime library is libLiteRtRuntimeCApi.so from Google's
# ai-edge-litert 1.4.0 wheel, pinned by sha256 FOR EACH CPU TYPE: the x86_64
# and the aarch64 (ARM64, e.g. a t4g box) wheels hold different files. The
# image is debian:bookworm-slim (glibc, libstdc++, libturbojpeg0); the model
# and the head are a read-only mount (/assets), the events log a volume
# (/events). It runs as user 65534, like the other services.
FROM python:3.12-slim AS litert
# TARGETARCH is set by BuildKit for the platform of the build; a stage gets
# it with ARG TARGETARCH and picks the file for its CPU
# (https://docs.docker.com/extensions/extensions-sdk/extensions/multi-arch/).
ARG TARGETARCH
RUN set -e; \
    case "${TARGETARCH}" in \
      amd64) want=f421e16d77de29ce886d8f0a9d946b79981d28fa183377c1cf0411ba7697433b ;; \
      arm64) want=23781d014a0e0de4f8dd40553e558e5f1248e29b1d135edeb187894588fd8d33 ;; \
      *) echo "no LiteRT pin for ${TARGETARCH}"; exit 1 ;; \
    esac; \
    pip download -q --no-deps --only-binary=:all: --disable-pip-version-check ai-edge-litert==1.4.0 -d /w; \
    python -m zipfile -e /w/ai_edge_litert-1.4.0-*.whl /w/x; \
    cp /w/x/ai_edge_litert/libLiteRtRuntimeCApi.so /libLiteRtRuntimeCApi.so; \
    echo "${want}  /libLiteRtRuntimeCApi.so" | sha256sum -c -

FROM golang:1.26-bookworm AS build
# libjpeg-turbo: the JPEG decoder Pillow uses. The tests with the tags run
# here: an image whose decode or resize is not Pillow's does not build.
RUN apt-get update -q && apt-get install -y -q --no-install-recommends libturbojpeg0-dev \
 && rm -rf /var/lib/apt/lists/*
COPY --from=litert /libLiteRtRuntimeCApi.so /usr/local/lib/libLiteRtRuntimeCApi.so
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd ./cmd
# the tiny model and its reference output: the LiteRT test runs in the build
COPY testdata ./testdata
ENV CGO_ENABLED=1 CGO_LDFLAGS="-L/usr/local/lib" LD_LIBRARY_PATH=/usr/local/lib
# Go cache mounts (https://docs.docker.com/build/cache/optimize/#use-cache-mounts).
# The tests run before ARG BUILD, so a new commit does not run them again when
# the code is the same (https://docs.docker.com/build/cache/invalidation/).
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    go test -tags "litert turbojpeg" -trimpath -count=1 ./...
ARG BUILD=dev
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    go build -tags "litert turbojpeg" -trimpath -ldflags="-s -w -X main.build=${BUILD}" -o /gate ./cmd/gate \
 && mkdir -p /events && chown 65534:65534 /events

FROM debian:bookworm-slim
RUN apt-get update -q && apt-get install -y -q --no-install-recommends libturbojpeg0 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=litert /libLiteRtRuntimeCApi.so /usr/lib/libLiteRtRuntimeCApi.so
COPY --from=build /gate /gate
COPY --from=build --chown=65534:65534 /events /events
USER 65534:65534
VOLUME ["/events"]
EXPOSE 8084
ENTRYPOINT ["/gate"]
CMD ["-listen", "0.0.0.0:8084", "-model", "/assets/gate_pe_s16_f16w_384.tflite", \
     "-head", "/assets/gate_head_pe_s16_384_deploy.json", "-events", "/events", "-threads", "1"]
