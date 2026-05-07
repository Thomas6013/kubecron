# Stage 1: generate templ templates
FROM golang:1.26-alpine AS templ-gen
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1001
WORKDIR /app
COPY . .
RUN templ generate

# Stage 2: build binary
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY --from=templ-gen /app .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kubecron ./cmd/kubecron

# Stage 3: minimal runtime (no shell, no CGO needed)
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/kubecron /kubecron
ENTRYPOINT ["/kubecron"]
