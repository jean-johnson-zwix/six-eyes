# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api ./cmd/main.go

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

WORKDIR /app

# Copy binary only — model artifacts are fetched from S3 at startup
COPY --from=builder /api .

EXPOSE 8080

CMD ["/app/api"]
