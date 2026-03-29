FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY go.mod ./
COPY . .
RUN go build -o /app/agente .

FROM alpine:latest
RUN apk add --no-cache php83 php83-cli curl
RUN curl -L https://github.com/phpstan/phpstan/releases/latest/download/phpstan.phar \
    -o /usr/local/bin/phpstan && chmod +x /usr/local/bin/phpstan
RUN ln -s /usr/bin/php83 /usr/bin/php
WORKDIR /app
COPY --from=builder /app/agente /app/agente
COPY phpstan.neon .
EXPOSE 8080
ENTRYPOINT ["/app/agente"]