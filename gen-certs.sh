#!/usr/bin/env bash
# Generate a self-signed cert for local testing into ./certs.
set -euo pipefail

CN="${1:-example.local}"
OUT="${OUT:-./certs}"

mkdir -p "$OUT"

openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$OUT/tls.key" \
  -out "$OUT/tls.crt" \
  -days 365 \
  -subj "/CN=$CN" \
  -addext "subjectAltName=DNS:$CN,DNS:localhost,IP:127.0.0.1"

echo "wrote $OUT/tls.crt and $OUT/tls.key (CN=$CN)"
