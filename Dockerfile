FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev git

COPY . .

RUN go mod download

RUN go build -o main /app/cmd/server/main.go

FROM alpine:latest

WORKDIR /root/

RUN apk add --no-cache libstdc++

COPY --from=builder /app/main .

EXPOSE ${SERVER_PORT}

CMD ["./main"]
