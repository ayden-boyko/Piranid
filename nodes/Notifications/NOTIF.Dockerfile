# Notification node build.
#
# Two changes from the previous version:
#   - the runtime stage is alpine, not golang:alpine. The old image carried a
#     whole Go toolchain, which does not fit the manifest's 128Mi limit.
#   - CGO_ENABLED=0 -trimpath, producing a small static binary.
FROM golang:1.26-alpine AS builder

WORKDIR /app

# The build needs the whole workspace: this module depends on pkg and pkg/node
# through replace directives, resolved via go.work.
COPY . .

WORKDIR /app/nodes/Notifications

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/notification_server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/notification_server /app/notification_server

# The schema is read at startup and must match NOTIFICATION_SCHEMA_PATH.
COPY --from=builder /app/nodes/Notifications/database /app/database

RUN chmod +x /app/notification_server

# Where NOTIFICATION_DB_PATH points. A volume keeps the notification history
# across restarts.
RUN mkdir -p /var/lib/piranid
VOLUME ["/var/lib/piranid"]

EXPOSE 8084

ENV NOTIFICATION_PORT=8084

# COURIER_TOKEN, AUTH_JWKS_URL, AUTH_ISSUER and AUTH_AUDIENCE are deliberately
# unset here: the server refuses to start without a Courier token, and refuses
# every RPC without an issuer and audience, rather than silently degrading.

CMD ["/app/notification_server"]