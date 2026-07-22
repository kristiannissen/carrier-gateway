FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o logistics-gateway ./cmd/api

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S gateway && adduser -S -G gateway gateway
WORKDIR /app
COPY --from=builder --chown=gateway:gateway /app/logistics-gateway .
USER gateway
EXPOSE 8080
ENTRYPOINT ["./logistics-gateway"]
