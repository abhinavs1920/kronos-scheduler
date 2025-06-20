# Build stage
FROM golang:1.24.2 AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o kube-scheduler ./cmd/scheduler

# Final stage
FROM alpine:3.18

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/kube-scheduler /usr/local/bin/kube-scheduler

# Create directory for config
RUN mkdir -p /etc/kubernetes

# Copy the scheduler config
COPY manifests/scheduler-config.yaml /etc/kubernetes/scheduler-config.yaml

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/kube-scheduler", "--config=/etc/kubernetes/scheduler-config.yaml"]