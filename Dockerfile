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
RUN CGO_ENABLED=0 GOOS=linux go build -o scheduler ./cmd/scheduler

# Final stage
FROM alpine:3.18

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/scheduler .

# Expose the port the app runs on
EXPOSE 8080

# Command to run the executable
CMD ["./scheduler"]