# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

# Set working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum first to cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code
COPY . .

# Build the binary statically so it runs perfectly in Alpine
# We target the entrypoint inside cmd/transcoder
RUN CGO_ENABLED=0 GOOS=linux go build -o /video-engine ./cmd/transcoder

# Stage 2: Create the minimal runtime image with FFmpeg
FROM alpine:latest

# Install FFmpeg (required for the Worker tier to slice and transcode video)
# Install ca-certificates (required for HTTPS calls to AWS S3/Cloudflare)
# Install bash (useful for debugging inside the container if needed)
RUN apk add --no-cache ffmpeg ca-certificates bash

# Set working directory
WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /video-engine /app/video-engine

# Make the binary executable (just in case)
RUN chmod +x /app/video-engine

# The engine can run as different roles based on arguments:
# e.g., /app/video-engine gateway, or /app/video-engine worker
ENTRYPOINT ["/app/video-engine"]
