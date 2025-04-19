FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY . ./

RUN go mod tidy

RUN go build -o main /app/cmd/server

FROM alpine:latest

WORKDIR /root/
RUN apk add --no-cache ffmpeg

COPY --from=builder /app/main .

EXPOSE ${SERVER_PORT}

CMD ["./main"]
