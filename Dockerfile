# Stage 1: build binary
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kubecron ./cmd/kubecron

# Stage 2: minimal runtime (no shell, no CGO needed)
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/kubecron /kubecron
ENTRYPOINT ["/kubecron"]
