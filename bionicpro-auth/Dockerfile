FROM golang:1.25-alpine AS builder

# Устанавливаем git и ca‑certificates
RUN apk add --no-cache git ca-certificates && update-ca-certificates

# Рабочая директория внутри контейнера
WORKDIR /src

# Кешируем загрузку зависимостей – ускоряем повторные билды
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь исходный код
COPY . .

# Параметры сборки: без CGO, linux/amd64, без отладочной информации
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-s -w" -o /app/bff .

FROM alpine:3.19 AS runtime

# Устанавливаем только сертификаты
RUN apk add --no-cache ca-certificates && update-ca-certificates

# Копируем бинарник из builder‑stage
COPY --from=builder /app/bff /usr/local/bin/bff

# Пользователь без прав root
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

# Открываем порт, который слушает приложение
EXPOSE 3000

# Точка входа
ENTRYPOINT ["/usr/local/bin/bff"]