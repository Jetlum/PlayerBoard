# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

FROM alpine:3.20 AS api
COPY --from=build /out/api /usr/local/bin/api
COPY migrations /migrations
ENV MIGRATIONS_DIR=/migrations
ENTRYPOINT ["api"]

FROM alpine:3.20 AS worker
COPY --from=build /out/worker /usr/local/bin/worker
ENTRYPOINT ["worker"]
