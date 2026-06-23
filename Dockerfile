# ---- build stage ----
FROM golang:1.23-alpine AS build

WORKDIR /src

# Cache deps first.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# Static binary, no CGO, so it runs on a minimal base image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/go-proxy-pass .

# ---- runtime stage ----
# distroless: no shell, small surface. Runs as nonroot (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/go-proxy-pass /usr/local/bin/go-proxy-pass

# Defaults; override with -e at run time.
ENV LISTEN_ADDR=":8443" \
    TLS_CERT_FILE="/etc/tls/tls.crt" \
    TLS_KEY_FILE="/etc/tls/tls.key" \
    PROXY_PROTOCOL="true"

EXPOSE 8443

ENTRYPOINT ["/usr/local/bin/go-proxy-pass"]
