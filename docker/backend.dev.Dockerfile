FROM golang:1.25-alpine

RUN apk add --no-cache git build-base

WORKDIR /app
