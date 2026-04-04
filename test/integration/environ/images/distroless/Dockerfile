# Stage 1: Build
FROM golang:1.24 AS builder
WORKDIR /app
COPY .. .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o app

# Stage 2: Minimal distroless
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
