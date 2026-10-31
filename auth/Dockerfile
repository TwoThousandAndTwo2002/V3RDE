# V3RDE auth service: a static Go binary on scratch; the data file is on a volume.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd ./cmd
# The commit the image is built from; the events log has it. It is the last
# input before the build, so a new commit does not run the steps above again
# (https://docs.docker.com/build/cache/invalidation/).
ARG BUILD=dev
# Go cache mounts: the standard library and the packages compile one time
# (https://docs.docker.com/build/cache/optimize/#use-cache-mounts).
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.build=${BUILD}" -o /auth ./cmd/auth \
 && mkdir -p /data /events && chown 65534:65534 /data /events

FROM scratch
COPY --from=build /auth /auth
# X49o: the CA roots, for TLS to the mail relay (smtp.gmail.com, STARTTLS); scratch has none
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# /data and /events are in the image, owned by the service user, so Docker
# fills a new named volume with that owner ("these files or directories are
# propagated (copied) into the volume by default":
# https://docs.docker.com/engine/storage/volumes/#mounting-a-volume-over-existing-data);
# an empty volume owned by root makes the service (not root) fail with
# "permission denied".
COPY --from=build --chown=65534:65534 /data /data
COPY --from=build --chown=65534:65534 /events /events
USER 65534:65534
VOLUME ["/data", "/events"]
EXPOSE 8081
ENTRYPOINT ["/auth"]
CMD ["-listen", "0.0.0.0:8081", "-db", "/data/auth.json"]
