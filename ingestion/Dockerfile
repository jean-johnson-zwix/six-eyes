FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /ingestion ./cmd/main.go

FROM alpine:3.19
# ca-certificates: required for TLS to Supabase and external APIs
# tzdata: required for correct UTC time handling
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /ingestion /ingestion
ENTRYPOINT ["/ingestion"]
