FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev
COPY . .

RUN go mod download


RUN go build -o main /app/cmd/server

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/main .

EXPOSE ${SERVER_PORT}

CMD ["./main"]
