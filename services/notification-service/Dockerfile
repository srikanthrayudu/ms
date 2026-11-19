# Multi-stage build for notification-service
FROM golang:alpine AS builder

WORKDIR /app

# Create data directory with nonroot permissions (uid 65532)
RUN mkdir -p /app/data && chown -R 65532:65532 /app/data

# Copy Go dependencies
COPY proto/ ./proto/
COPY shared/ ./shared/
COPY services/notification-service/ ./services/notification-service/

# Initialize workspace for only the required modules
RUN go work init ./proto ./shared ./services/notification-service

# Build statically linked binary
RUN cd services/notification-service && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/notification-service ./cmd/notification-service

# Runtime stage: distroless static nonroot
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder --chown=65532:65532 /app/data /app/data
COPY --from=builder /bin/notification-service /app/notification-service

USER nonroot:nonroot

EXPOSE 50056

ENTRYPOINT ["/app/notification-service"]
