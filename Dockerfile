FROM alpine:latest

# Set working directory
WORKDIR /app

# Copy the Linux binary (rename it to "tsk" for convenience)
COPY build/tsk_linux_amd64_v0-0-2 /app/tsk

# Ensure the binary is executable
RUN chmod +x /app/tsk

# Set the default command to run your program
ENTRYPOINT ["/app/tsk"]
