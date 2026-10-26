# Multi-stage Dockerfile for Kaggle Workaround Relay
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/kaggle-workaround ./cmd/relay

# Final runtime image
FROM alpine:3.20

RUN apk add --no-cache bash ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/kaggle-workaround /usr/local/bin/kaggle-workaround

# Single open port for Machine B relay
EXPOSE 9090

ENTRYPOINT ["kaggle-workaround"]
CMD ["server", "-bind", "0.0.0.0", "-port", "9090"]
