FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the entire repo into the image
COPY . .

# Change to the Logging module directory
WORKDIR /app/nodes/Logging

# Build the binary
RUN go build -o /app/logging_server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/logging_server .
#give permissions to the binary
RUN chmod +x logging_server
# Expose port 8082.
EXPOSE 8082

# Set environment variables (as before)
ENV LOGGING_PORT=8002
ENV DB_PORT=8103
ENV DB_TOLKEN=a8f1e3c7b2d4f9a6e5c1b8d3f7e2a4c5
ENV DB_ORG=LOGGING
ENV DB_BUCKET=LOGGING

CMD ["./logging_server"]
