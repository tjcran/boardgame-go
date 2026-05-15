# Multi-stage, multi-arch build for boardgame-mcp.
#
#   # single-arch (host platform):
#   docker build -t boardgame-mcp -f mcp/deploy/Dockerfile .
#   docker run --rm -p 8080:8080 boardgame-mcp serve --transport=http --port=8080
#
#   # multi-arch (used by the release CI workflow):
#   docker buildx build --platform linux/amd64,linux/arm64 \
#       -f mcp/deploy/Dockerfile -t ghcr.io/owner/boardgame-mcp:tag --push .
#
# The image is distroless/static (no shell, no package manager) and runs as
# the nonroot user. Final image is ~15 MB.

# --platform=$BUILDPLATFORM keeps the builder stage running natively on
# whatever host invoked the build (no QEMU overhead); we cross-compile
# the Go binary to TARGETOS/TARGETARCH explicitly below.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Copy go.work + both go.mod/go.sum pairs before sources so `go mod
# download` caches independently of source changes.
COPY go.work ./
COPY go.mod go.sum ./
COPY mcp/go.mod mcp/go.sum mcp/
RUN go mod download

# Source.
COPY . .

# CGO_ENABLED=0 keeps the binary fully static; modernc.org/sqlite is pure Go
# so no toolchain is needed at runtime. -s -w strips debug info (~5 MB savings).
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" \
    -o /out/boardgame-mcp ./mcp/cmd/boardgame-mcp

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/boardgame-mcp /boardgame-mcp
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/boardgame-mcp"]
CMD ["serve", "--transport=http", "--port=8080"]
