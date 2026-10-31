# The backend image: one static binary on distroless, no shell (ADR 0001). Go cross-compiles on the build
# machine for each target platform, so a multi-platform build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS build
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

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
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
