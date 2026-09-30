# DockIt container image: the static dockit binary on distroless, nothing else.
#
#   podman build --build-arg VERSION=$(git describe --tags --always --dirty) -t dockit .
#
# See "Run DockIt in a container" in README.md and OPERATIONS.md.

ARG GO_VERSION=1.27

FROM --platform=$BUILDPLATFORM docker.io/library/golang:${GO_VERSION} AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# A release's major.minor must match the dataset format it writes.
RUN go run ./internal/buildcheck "${VERSION}"
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/dockit ./cmd/dockit
# The dataset mount point, owned by the distroless nonroot user, so a new named
# volume mounted on it starts out writable by it.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dockit /usr/local/bin/dockit
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV DOCKIT_DATA=/data \
    DOCKIT_LISTEN=:8080
VOLUME /data
EXPOSE 8080
# serve shuts down within 10 seconds of SIGTERM; run with --stop-timeout 15 or
# more so podman does not SIGKILL it first and leave dockit.lock behind.
STOPSIGNAL SIGTERM
ENTRYPOINT ["dockit"]
CMD ["serve"]
