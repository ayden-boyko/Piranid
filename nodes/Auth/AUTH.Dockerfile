# Auth node build.
#
# The signing key is deliberately NOT baked in. It is mounted at runtime from a
# Kubernetes Secret: an image layer is readable by anyone who can pull the
# image, and a key in an image cannot be rotated without a new build.
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY . .

WORKDIR /app/nodes/Auth

# Static build: the runtime stage has no Go toolchain.
RUN CGO_ENABLED=0 go build -trimpath -o /app/auth_server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/auth_server /app/auth_server

# The schema is read at startup to create tables if they are missing.
# Kept under /app/database/ so the AUTH_SCHEMA_PATH default resolves.
COPY --from=builder /app/nodes/Auth/database /app/database

RUN chmod +x /app/auth_server

# Where the signing-key Secret is mounted. Must match AUTH_PRIVATE_KEY_PATH in
# manifests/auth-deployment.yaml.
RUN mkdir -p /etc/piranid/keys
VOLUME ["/etc/piranid/keys"]

# Where the SQLite database lives. Must match AUTH_DB_PATH.
RUN mkdir -p /var/lib/piranid
VOLUME ["/var/lib/piranid"]

EXPOSE 8081

ENV AUTH_PORT=8081

# Token configuration. AUTH_ISSUER and AUTH_AUDIENCE are deliberately not set
# here: they are deployment-specific, and a container that silently starts with
# an empty issuer would issue tokens no service can verify. The node refuses to
# start without them.

CMD ["/app/auth_server"]