FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Notifications module directory
WORKDIR /app/nodes/Event_Queue

# Build the binary
RUN go build -o /app/event_server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/event_server .
COPY --from=builder /app/nodes/Event_Queue/database/* /app/

#give permissions to the binary
RUN chmod +x event_server
# Expose port 8082.
EXPOSE 8082

WORKDIR /app
RUN ls -l /app

# Set environment variables (as before)
ENV EVENT_QUEUE_PORT=8002

CMD ["./event_server"]
