# syntax=docker/dockerfile:1.7
# Multi-stage build for commonplace. Pure-Go SQLite (modernc.org/sqlite) means
# no CGO, so the final image can be tiny (~25 MB on alpine).

FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/commonplace ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/commonplace /app/commonplace
# The SQLite db lives on a mounted volume at /data.
ENV DB_PATH=/data/commonplace.db
ENV ADDR=:8080
EXPOSE 8080
CMD ["/app/commonplace"]
