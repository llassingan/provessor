# Stage 1: Build Go binary
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates openssl
WORKDIR /app
COPY --from=builder /api .
COPY ansible/ ansible/
RUN mkdir -p /app/data
EXPOSE 10000
CMD ["./api"]
