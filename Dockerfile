# --- Stage 1: Builder ---
FROM golang:1.25.3-alpine AS builder

# Устанавливаем необходимые системные зависимости (git нужен для скачивания модулей)
RUN apk add --no-cache git

WORKDIR /app

# Копируем файлы зависимостей и скачиваем их (кэширование слоев Docker)
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем бинарник
# CGO_ENABLED=0 создает статически слинкованный файл, который работает на "голом" Alpine
RUN CGO_ENABLED=0 GOOS=linux go build -o ton-s3-gateway cmd/adapter/main.go

# --- Stage 2: Runtime ---
FROM alpine:latest

# Устанавливаем корневые сертификаты (нужны для HTTPS запросов к TON Config и Webhooks)
RUN apk add --no-cache ca-certificates

WORKDIR /root/

# Копируем бинарник из стадии сборки
COPY --from=builder /app/ton-s3-gateway .

# Копируем схему БД (опционально, если захотите делать миграции из Go, но мы сделаем это через Postgres контейнер)
# COPY schema.sql . 

# Создаем папки для данных
RUN mkdir -p ./var/ton-db ./var/downloads

# Объявляем порты
# 8080 - S3 Gateway
# 3000 - Admin API
# 14321/udp - TON ADNL (P2P сеть)
EXPOSE 8080 3000 14321/udp

# Запуск
CMD ["./ton-s3-gateway"]