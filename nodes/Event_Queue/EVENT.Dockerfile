# Event Queue node build.
#
# Two changes from the previous version:
#   - the runtime stage is alpine, not golang:alpine. The old image carried a
#     whole Go toolchain, which does not fit the manifest's 256Mi limit.
#   - CGO_ENABLED=0 -trimpath, producing a small static binary.
FROM golang:1.26-alpine AS builder

WORKDIR /app

# The build needs the whole workspace: this module depends on pkg and pkg/node
# through replace directives, resolved via go.work.
COPY . .

WORKDIR /app/nodes/Event_Queue

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/event_server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/event_server /app/event_server

RUN chmod +x /app/event_server

EXPOSE 8082

ENV EVENT_QUEUE_PORT=8082

# RABBIT_MQ_PORT has a default in main.go, so an unset value logs a warning
# rather than panicking. RabbitMQ host and credentials are configurable.
CMD ["/app/event_server"]