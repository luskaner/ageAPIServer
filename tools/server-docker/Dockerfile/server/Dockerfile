# syntax=docker/dockerfile:1

# Compile
FROM golang:1.27-alpine3.24 AS compiler
ARG VERSION=development
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o build/ageAPIServer ./cmd/ageAPIServer
RUN cp -r resources build/resources && mkdir -p build/resources/certificates
# Compress
FROM alpine:3.24 AS compressor
RUN apk add --no-cache upx
WORKDIR /app
COPY --from=compiler /app/build/ageAPIServer .
RUN upx --lzma ageAPIServer
# Execute
FROM alpine:3.24
# Plain HTTP: the game clients speak it, terminate TLS in front if needed.
EXPOSE 8080/tcp
VOLUME /app/resources/config
VOLUME /app/resources/certificates
VOLUME /app/uploads
VOLUME /app/logs
VOLUME /app/data
WORKDIR /app
COPY --from=compiler /app/build/resources resources
COPY --from=compressor /app/ageAPIServer .
ENTRYPOINT ["./ageAPIServer"]
