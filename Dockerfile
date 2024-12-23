FROM golang:1.21-alpine

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./

RUN go mod download

COPY . .

EXPOSE 8080
RUN go build -o main /app/cmd/server

CMD ["./main"]