FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Notifications module directory
WORKDIR /app/nodes/Notifications

# Build the binary
RUN go build -o /app/notification_server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/notification_server .
#give permissions to the binary
RUN chmod +x notification_server
# Expose port 8000.
EXPOSE 8000

# Set environment variables (as before)
ENV NOTIFICATION_PORT=8004
ENV COURIER_TOLKEN=pk_prod_BY39M20QJHMMV3QWC2MY5MNNB3WQ


CMD ["./notification_server"]
