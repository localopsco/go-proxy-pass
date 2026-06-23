# go-proxy-pass

A small Go HTTPS service that **terminates TLS itself**, sitting behind **nginx
SSL passthrough**. nginx never decrypts the traffic — it forwards the raw TLS
bytes and tells the Go service the **real client IP** via the PROXY protocol.

## How it fits together

```
client ──443/TLS──▶ nginx (stream, ssl_preread)  ──8443/TLS──▶ go app
                    • does NOT terminate TLS                    • terminates TLS
                    • peeks ClientHello for routing             • reads real client IP
                    • prepends PROXY protocol header              from PROXY header
```

Because TLS is terminated in the Go service (not at nginx), there is **no
trustworthy `X-Forwarded-For`** — nginx can't add one without decrypting. The
PROXY protocol solves this: nginx prepends a tiny header naming the original
client, and the Go service rewrites the connection's remote address from it.
The handler then just reads `r.RemoteAddr`.

## Files

| File | Purpose |
|------|---------|
| `main.go` | TLS-terminating HTTPS server; parses PROXY protocol for the real client IP |
| `Dockerfile` | Multi-stage build → distroless nonroot image |
| `nginx.conf` | `stream` block: `ssl_preread` passthrough + `proxy_protocol on` |
| `docker-compose.yml` | Runs nginx + app together for an end-to-end demo |
| `gen-certs.sh` | Self-signed cert into `./certs` for local testing |

## Configuration (env vars)

| Var | Default | Meaning |
|-----|---------|---------|
| `LISTEN_ADDR` | `:8443` | listen address |
| `TLS_CERT_FILE` | `/etc/tls/tls.crt` | PEM certificate |
| `TLS_KEY_FILE` | `/etc/tls/tls.key` | PEM private key |
| `PROXY_PROTOCOL` | `true` | require PROXY protocol header (set `false` to hit the app directly) |

## Run the full demo

```bash
./gen-certs.sh                 # writes ./certs/tls.{crt,key}
docker compose up --build

# In another terminal — nginx is the only ingress, on :443
curl -k --resolve example.local:443:127.0.0.1 https://example.local/
```

You'll see your real client IP in the response and in the app logs, not nginx's
container IP.

## Run just the Go app (no nginx)

PROXY protocol is required by default, so disable it to curl the app directly:

```bash
docker build -t go-proxy-pass .
docker run --rm -p 8443:8443 \
  -e PROXY_PROTOCOL=false \
  -v "$PWD/certs:/etc/tls:ro" \
  go-proxy-pass

curl -k https://localhost:8443/
```

## Notes

- The PROXY protocol `Policy` is set to `REQUIRE` — connections without the
  header are rejected. That's the safe default once nginx is the only ingress.
  Use `OPTIONAL` (or `PROXY_PROTOCOL=false`) while testing direct connections.
- `ssl_preread on;` in nginx reads the ClientHello *without* decrypting, so you
  can route by SNI (`$ssl_preread_server_name`) while still passing TLS through.
