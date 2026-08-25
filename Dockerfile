# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/examnode ./cmd/examnode

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/examnode /app/examnode
COPY migrations /app/migrations
EXPOSE 8080
USER app
ENTRYPOINT ["/app/examnode"]
