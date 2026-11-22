# The backend image: one static binary on distroless, no shell (ADR 0001). Go cross-compiles on the build
# machine for each target platform, so a multi-platform build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -buildid=" -o /out/backend ./cmd/backend

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
LABEL org.opencontainers.image.source="https://github.com/Reference-Systems-Lab/commerce-backend" \
      org.opencontainers.image.title="commerce-backend" \
      org.opencontainers.image.description="The commerce platform's backend API" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/backend /backend
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s --retries=3 CMD ["/backend", "healthcheck"]
ENTRYPOINT ["/backend"]
CMD ["serve"]
