# Multi-stage build for api-gateway
FROM golang:alpine AS builder

WORKDIR /app

# Copy Go dependencies
COPY proto/ ./proto/
COPY shared/ ./shared/
COPY services/api-gateway/ ./services/api-gateway/

# Initialize workspace for only the required modules
RUN go work init ./proto ./shared ./services/api-gateway

# Build statically linked binary
RUN cd services/api-gateway && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/api-gateway ./cmd/api-gateway

# Runtime stage: distroless static nonroot
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder /bin/api-gateway /app/api-gateway

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app/api-gateway"]
