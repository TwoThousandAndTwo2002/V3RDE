# V3RDE analyze server: a static Go binary on scratch. No shell, no package
# manager and no secrets in the image: the Meta key and the bearer token are
# files mounted at run time.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd ./cmd
# Go cache mounts: the standard library and the packages compile one time
# (https://docs.docker.com/build/cache/optimize/#use-cache-mounts).
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /analyze ./cmd/analyze

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /analyze /analyze
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/analyze"]
CMD ["-listen", "0.0.0.0:8080", "-key-file", "/run/secrets/meta_key", \
     "-token-file", "/run/secrets/analyze_token", "-metrics-dir", "/metrics"]
