FROM golang:1.25.3-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o ton-s3-gateway cmd/adapter/main.go

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /root/

COPY --from=builder /app/ton-s3-gateway .


RUN mkdir -p ./var/ton-db ./var/downloads

EXPOSE 8080 3000 14321/udp 14321/tcp

CMD ["./ton-s3-gateway"]