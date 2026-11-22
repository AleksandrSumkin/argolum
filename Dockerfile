# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Copy go modules manifests
COPY go.mod go.sum ./
RUN go mod download

# Copy Go source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/bin/argolum ./cmd/argolum && \
    rm -rf /go/pkg /root/.cache

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /app/bin/argolum /argolum

USER 65532:65532

ENTRYPOINT ["/argolum"]
