FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Notifications module directory
WORKDIR /app/nodes/Auth

# Build the binary
RUN go build -o /app/auth_server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/auth_server .
COPY --from=builder /app/nodes/Auth/database/* /app/

#give permissions to the binary
RUN chmod +x auth_server
# Expose port 8081.
EXPOSE 8081

WORKDIR /app
RUN ls -l /app

# Set environment variables (as before)
ENV AUTH_PORT=8001
ENV COURIER_TOLKEN=pk_prod_BY39M20QJHMMV3QWC2MY5MNNB3WQ


CMD ["./auth_server"]
