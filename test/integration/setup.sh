#!/bin/bash
# Setup script for integration tests
# This script initializes test data in all emulated services

set -e

# Wait for services to be healthy
echo "Waiting for services to be ready..."

# LocalStack (AWS)
until curl -s http://localhost:4566/_localstack/health | grep -q '"s3": *"available"'; do
  echo "Waiting for LocalStack..."
  sleep 2
done
echo "✓ LocalStack is ready"

# Azurite (Azure)
until nc -z localhost 10000 2>/dev/null; do
  echo "Waiting for Azurite..."
  sleep 2
done
echo "✓ Azurite is ready"

# Fake GCS
until curl -s http://localhost:4443/storage/v1/b >/dev/null 2>&1; do
  echo "Waiting for Fake GCS..."
  sleep 2
done
echo "✓ Fake GCS is ready"

# Consul
until consul members >/dev/null 2>&1; do
  echo "Waiting for Consul..."
  sleep 2
done
echo "✓ Consul is ready"

# etcd
until etcdctl endpoint health >/dev/null 2>&1; do
  echo "Waiting for etcd..."
  sleep 2
done
echo "✓ etcd is ready"

echo ""
echo "=== Setting up test data ==="

# Sample config for testing
CONFIG_YAML='version: "1.0"
rules:
  - id: test-redirect
    match:
      type: exact
      path: /old
    redirect:
      to: https://new.example.com/
      status: 301
'

# === AWS S3 ===
echo "Setting up S3..."
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
export AWS_ENDPOINT_URL=http://localhost:4566

aws --endpoint-url=http://localhost:4566 s3 mb s3://test-config 2>/dev/null || true
echo "$CONFIG_YAML" | aws --endpoint-url=http://localhost:4566 s3 cp - s3://test-config/redirector.yaml
echo "✓ S3 bucket and config created"

# === AWS Parameter Store ===
echo "Setting up Parameter Store..."
aws --endpoint-url=http://localhost:4566 ssm put-parameter \
  --name "/redirector/config" \
  --value "$CONFIG_YAML" \
  --type String \
  --overwrite 2>/dev/null || true
echo "✓ Parameter Store config created"

# === AWS Secrets Manager ===
echo "Setting up Secrets Manager..."
aws --endpoint-url=http://localhost:4566 secretsmanager create-secret \
  --name "redirector/config" \
  --secret-string "$CONFIG_YAML" 2>/dev/null || \
aws --endpoint-url=http://localhost:4566 secretsmanager put-secret-value \
  --secret-id "redirector/config" \
  --secret-string "$CONFIG_YAML"
echo "✓ Secrets Manager secret created"

# === Azure Blob Storage ===
echo "Setting up Azure Blob Storage..."
# Azurite default connection string
AZURE_CONN="DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;"

# Create container and upload config using az CLI if available
if command -v az &> /dev/null; then
  az storage container create --name configs --connection-string "$AZURE_CONN" 2>/dev/null || true
  echo "$CONFIG_YAML" | az storage blob upload \
    --container-name configs \
    --name redirector.yaml \
    --data @- \
    --connection-string "$AZURE_CONN" \
    --overwrite 2>/dev/null || true
  echo "✓ Azure Blob container and config created"
else
  echo "⚠ Azure CLI not found, skipping Azure setup (will be done in test)"
fi

# === Fake GCS ===
echo "Setting up Fake GCS..."
# Create bucket via API
curl -s -X POST "http://localhost:4443/storage/v1/b?project=test" \
  -H "Content-Type: application/json" \
  -d '{"name": "test-config"}' >/dev/null 2>&1 || true

# Upload config
curl -s -X POST "http://localhost:4443/upload/storage/v1/b/test-config/o?uploadType=media&name=redirector.yaml" \
  -H "Content-Type: application/x-yaml" \
  -d "$CONFIG_YAML" >/dev/null 2>&1 || true
echo "✓ GCS bucket and config created"

# === Consul ===
echo "Setting up Consul..."
consul kv put redirector/config "$CONFIG_YAML"
echo "✓ Consul KV config created"

# === etcd ===
echo "Setting up etcd..."
etcdctl put /redirector/config "$CONFIG_YAML"
echo "✓ etcd config created"

echo ""
echo "=== Setup complete! ==="
echo "Run integration tests with: go test -tags=integration ./test/integration/..."
