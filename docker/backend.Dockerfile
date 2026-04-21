FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY . .
RUN go build -o main .

FROM alpine:latest

RUN printf '%s\n%s\n' \
    'https://mirrors.tuna.tsinghua.edu.cn/alpine/latest-stable/main' \
    'https://mirrors.tuna.tsinghua.edu.cn/alpine/latest-stable/community' \
    > /etc/apk/repositories \
 && apk --no-cache add ca-certificates
WORKDIR /app

COPY --from=builder /app/main .

CMD ["./main"]
