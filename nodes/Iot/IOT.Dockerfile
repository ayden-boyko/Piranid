FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Notifications module directory
WORKDIR /app/nodes/Iot

# Build the binary
RUN go build -o /app/iot_server .

FROM golang:1.26-alpine

WORKDIR /app
COPY --from=builder /app/iot_server .
COPY --from=builder /app/nodes/Iot/database/* /app/

#give permissions to the binary
RUN chmod +x iot_server
# Expose port 8085.
EXPOSE 8085

WORKDIR /app
RUN ls -l /app

# Set environment variables (as before)
ENV IOT_PORT=8085

CMD ["./iot_server"]