FROM golang:1.24-alpine AS builder

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
COPY --from=builder /app/nodes/Notifications/database/* /app/

#give permissions to the binary
RUN chmod +x notification_server
# Expose port 8084.
EXPOSE 8084

WORKDIR /app
RUN ls -l /app

# Set environment variables (as before)
ENV NOTIFICATION_PORT=8004
ENV COURIER_TOKEN=pk_prod_BY39M20QJHMMV3QWC2MY5MNNB3WQ


CMD ["./notification_server"]
