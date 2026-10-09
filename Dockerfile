# The backend image: one static binary on distroless, no shell (ADR 0001). Go cross-compiles on the build
# machine for each target platform, so a multi-platform build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.2-trixie@sha256:e58d6f83b3416618d8bcac2b3dde1b7f7e3c4a77d25e88637f8bbae81536c48d AS build
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

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
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
