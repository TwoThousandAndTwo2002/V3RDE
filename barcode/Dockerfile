# V3RDE barcode lookup. SQLite (mattn/go-sqlite3, cgo) is linked
# statically against musl, so the final image is scratch, like auth and
# listings: no shell, no libc, one binary. The catalog is a read-only mount
# (/assets) and the events log a volume (/events). The tests with the sqlite
# tag run in the build: an image whose SQLite path fails them does not build.
FROM golang:1.26-alpine AS build
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY *.go ./
COPY cmd ./cmd
ENV CGO_ENABLED=1
# Go cache mounts (https://docs.docker.com/build/cache/optimize/#use-cache-mounts).
# The tests run before ARG BUILD, so a new commit does not run them again when
# the code is the same (https://docs.docker.com/build/cache/invalidation/);
# the same flags as the build (-trimpath, tags), so it is one compile.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    go test -tags "sqlite sqlite_omit_load_extension" -trimpath -count=1 ./...
ARG BUILD=dev
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    go build -tags "sqlite sqlite_omit_load_extension" -trimpath \
      -ldflags="-s -w -X main.build=${BUILD} -linkmode external -extldflags -static" \
      -o /barcode ./cmd/barcode \
 && mkdir -p /events /sock && chown 65534:65534 /events /sock

FROM scratch
COPY --from=build /barcode /barcode
COPY --from=build --chown=65534:65534 /events /events
# US: the socket folder, owned by the service user, so a new v3rde_sock volume gets that owner (as /events)
COPY --from=build --chown=65534:65534 /sock /sock
USER 65534:65534
VOLUME ["/events"]
EXPOSE 8083
ENTRYPOINT ["/barcode"]
CMD ["-listen", "0.0.0.0:8083", "-catalog", "/assets/products.sqlite", "-events", "/events"]
