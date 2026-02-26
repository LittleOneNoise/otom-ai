# Étape 1 : Compilation
FROM golang:1.25-alpine3.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o otomai .

# Étape 2 : Exécution
FROM alpine:3.23
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/otomai .
COPY --from=builder /app/.env . 

CMD ["./otomai"]