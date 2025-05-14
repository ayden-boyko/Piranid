FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Notifications module directory
WORKDIR /app/controllers

# Build the binary
RUN go build -o /app/control_server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/control_server .
COPY --from=builder /app/controllers/database/* /app/

#give permissions to the binary
RUN chmod +x control_server
# Expose port 8080.
EXPOSE 8080

WORKDIR /app
RUN ls -l /app

# Set environment variables (as before)
ENV CONTROLLER_PORT=8000

CMD ["./control_server"]
