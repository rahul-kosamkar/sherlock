FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /sherlock ./cmd/sherlock

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S sherlock && adduser -S sherlock -G sherlock

COPY --from=builder /sherlock /usr/local/bin/sherlock
COPY migrations /migrations

USER sherlock

EXPOSE 8080

ENTRYPOINT ["sherlock"]
CMD ["serve"]
