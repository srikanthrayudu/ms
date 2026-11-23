# Multi-stage build for ride-service
FROM golang:alpine AS builder

WORKDIR /app

# Create data directory with nonroot permissions (uid 65532)
RUN mkdir -p /app/data && chown -R 65532:65532 /app/data

# Copy Go dependencies
COPY proto/ ./proto/
COPY shared/ ./shared/
COPY services/ride-service/ ./services/ride-service/

# Copy the prebuilt WASM fare-engine binary (built by `task wasm:build`)
COPY wasm/fare-engine/fare-engine.wasm ./wasm/fare-engine/fare-engine.wasm

# Initialize workspace for only the required modules
RUN go work init ./proto ./shared ./services/ride-service

# Build statically linked binary
RUN cd services/ride-service && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/ride-service ./cmd/ride-service

# Runtime stage: distroless static nonroot
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder --chown=65532:65532 /app/data /app/data
COPY --from=builder /bin/ride-service /app/ride-service
# Embed the WASM fare-engine binary at the default engine path.
# Can be overridden by mounting a Kubernetes ConfigMap at /app/wasm/
# for zero-restart hot-reload of pricing logic.
COPY --from=builder /app/wasm/fare-engine/fare-engine.wasm /app/wasm/fare-engine.wasm

USER nonroot:nonroot

ENV WASM_FARE_ENGINE_PATH=/app/wasm/fare-engine.wasm

EXPOSE 50054

ENTRYPOINT ["/app/ride-service"]
