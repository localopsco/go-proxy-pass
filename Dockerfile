# ---- build stage ----
FROM golang:1.23-alpine AS build

WORKDIR /src

# Cache deps first.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# Static binary, no CGO, so it runs on a minimal base image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/go-proxy-pass .

# Bake a self-signed cert into the image so the container starts without an
# external mount (k8s Secret, compose volume, etc). TEMPORARY: this ships a
# private key inside the image and the cert won't be trusted by clients — fine
# for getting unblocked / internal passthrough, NOT for production. Mounting a
# real cert at /etc/tls still overrides these (a volume mount shadows the
# baked-in files).
ARG CERT_CN=localhost
RUN apk add --no-cache openssl && \
    mkdir -p /out/tls && \
    openssl req -x509 -newkey rsa:2048 -nodes \
      -keyout /out/tls/tls.key \
      -out /out/tls/tls.crt \
      -days 365 \
      -subj "/CN=${CERT_CN}" \
      -addext "subjectAltName=DNS:${CERT_CN},DNS:localhost,IP:127.0.0.1" && \
    chmod 0644 /out/tls/tls.key /out/tls/tls.crt

# ---- runtime stage ----
# distroless: no shell, small surface. Runs as nonroot (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/go-proxy-pass /usr/local/bin/go-proxy-pass

# Baked-in self-signed cert (see build stage). 0644 so the nonroot user can
# read the key; override by mounting a real cert at /etc/tls.
COPY --from=build /out/tls/ /etc/tls/

# Defaults; override with -e at run time.
ENV LISTEN_ADDR=":8443" \
    TLS_CERT_FILE="/etc/tls/tls.crt" \
    TLS_KEY_FILE="/etc/tls/tls.key" \
    PROXY_PROTOCOL="true"

EXPOSE 8443

ENTRYPOINT ["/usr/local/bin/go-proxy-pass"]
