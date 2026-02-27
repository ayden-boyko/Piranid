FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Notifications module directory
WORKDIR /app/nodes/Remote_DB

# Build the binary
RUN go build -o /app/remote_db .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/remote_db .
COPY --from=builder /app/nodes/Remote_DB/database/* /app/

#give permissions to the binary
RUN chmod +x remote_db
# Expose port 8081.
EXPOSE 8081

WORKDIR /app
RUN ls -l /app

# Set environment variables (as before)
ENV REMOTE_DB_PORT=8001

CMD ["./remote_db"]
