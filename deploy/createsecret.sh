
#!/bin/bash

cd "${0%/*}"

set -e
echo "Create opa-server TLS secret.Communication between Kubernetes and OPA must be secured using TLS. To configure TLS"

read -p "Press enter to continue"

rm -rf ./secret
mkdir ./secret
cd ./secret

openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -days 100000 -out ca.crt -subj "/CN=admission_ca"
cat >server.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth, serverAuth
EOF
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -subj "/CN=opa.opa.svc" -config server.conf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 100000 -extensions v3_req -extfile server.conf
cd ..

# create k8s opa secret
kubectl -n opa create secret tls opa-server --cert=./secret/server.crt --key=./secret/server.key