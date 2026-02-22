FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /wasmdb ./cmd/wasmdb

FROM alpine:3.21

RUN apk --no-cache add ca-certificates
COPY --from=builder /wasmdb /usr/local/bin/wasmdb

RUN adduser -D -u 1000 wasmdb
USER wasmdb

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/wasmdb"]
