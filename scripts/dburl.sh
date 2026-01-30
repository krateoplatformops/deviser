#!/usr/bin/env bash

kubectl create secret generic eventrouter-db \
  --from-literal=DB_URL="DB_URL=postgres://produser:prodpass@postgres.demo-system.svc.cluster.local:5432/proddb?sslmode=disable" \
  --namespace demo-system
