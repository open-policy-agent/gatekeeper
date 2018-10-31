# kubernetes-policy-controller

Kubernetes compliance is enforced at the “runtime” via tools such as network policy and pod security policy. [kubernetes-policy-controller](https://github.com/Azure/kubernetes-policy-controller) extends the compliance enforcement at “create” event not at “run“ event, some of the examples are "Minimum replica count enforcement", "White listed/ black listed registries", "not allowing conflicting hosts for ingresses". Kubernetes allows decoupling complex logic such as policy decision from the inner working of API Server by means of "admission controllers”. Admission control is a custom logic executed by a webhook. `Kubernetes policy controller` is a mutating and a validating webhook which gets called for matching Kubernetes API server requests by the admission controller to enforce semantic validation of objects during create, update, and delete operations. It uses Open Policy Agent ([OPA](https://github.com/open-policy-agent/opa)) is a policy engine for Cloud Native environments hosted by CNCF as a sandbox level project.

The administrator of the cluster defines the policy which is enforced by the `kubernetes-policy-controller`. There are two type of policies namely `validation` e.g. white listed registries and `mutation` e.g. annotating objects created in a namespace.

Lets lets look at the example below which implements a validation policy to ensure Ingress hostnames must be unique across Namespaces.

## deploy `kubernetes-policy-controller' on a kubernetes cluster

Prerequisites are that you have a kubernets cluster (e.g ACS Engine or Azure Kubernetes Cluster (AKS))
To implement admission control rules that validate Kubernetes resources during create, update, and delete operations, you must enable the [ValidatingAdmissionWebhook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#validatingadmissionwebhook) when the Kubernetes API server is started. the admission controller is included in the [recommended set of admission controllers to enable](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#is-there-a-recommended-set-of-admission-controllers-to-use)

### 1.  create opa namespace

```bash
kubectl create ns opa
```

### 2.  create tls secret for `kubernetes-policy-controller`

```bash
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

kubectl -n opa create secret tls opa-server --cert=./server.crt --key=./server.key

kubectl apply -n opa -f ./opa.yaml
```

### 3. deploy default kubernetes policies

```bash
cat > ./matches.rego <<EOF
package k8s
import data.kubernetes

matches[[kind, namespace, name, resource]] {
    resource := kubernetes[kind][namespace][name]
}
EOF
kubectl -n opa create configmap kubernetes-matches --from-file=./matches.rego
```

### 4. enable webhook confuguration

```bash
cat > ./validating-webhook-configuration.yaml <<EOF
kind: ValidatingWebhookConfiguration
apiVersion: admissionregistration.k8s.io/v1beta1
metadata:
  name: validating.kubernetes-policy-controller
webhooks:
  - name: validating.webhook.kubernetes-policy-controller
    rules:
      - operations: ["CREATE", "UPDATE"]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
    clientConfig:
      caBundle: $(cat ./ca.crt | base64 | tr -d '\n')
      service:
        namespace: opa
        name: opa
        path: "/v1/validate"
EOF

kubectl -n opa apply -f ./validating-webhook-configuration.yaml
```

## apply the ingress conflict policy definition

The policy is defined in Rego a policy language used by the Open Policy Agent. Each validation policy is a deny rule. In this case, there is a violation if any two ingresses in different namespaces have the same host. 

Store the policy in Kubernetes as a ConfigMap. This is automatically uploaded to the policy engine.

```bash
cat > ./ingress-conflict.rego <<EOF
deny[{
    "id": "ingress-conflict",
    "resource": {"kind": "ingresses", "namespace": namespace, "name": name},
    "message": "ingress host conflicts with an existing ingress",
}] {
    # gets the ingress matching the ingress that needs to be validated
    matches[["ingresses", namespace, name, matched_ingress]]
    # gets any other ingress which is already a part of the cluster
    matches[["ingresses", other_ns, other_name, other_ingress]]
    # filters to ingresses in other namespaces
    namespace != other_ns
    other_ingress.spec.rules[_].host == matched_ingress.spec.rules[_].host
}
EOF
kubectl create configmap ingress-conflict --from-file=ingress-conflict.rego

```

## create ingress in default namespace

This operation will succeed as there is no conflicting ingress host in any other namespace

```bash
cat > ./ingress-host.yaml <<EOF
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: ingress-host
spec:
  rules:
  - host: acmecorp.com
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
EOF
kubectl apply -f ./ingress-host.yaml
```

## Create ingress with same host in another namespace (This should fail)

Create a test namespace 

```bash
kubectl create ns test
```

Try create a 

```bash 
kubectl -n test apply -f ./ingress-host.yaml
```

This is the error message returned by the `kubernetes-policy-controller`

```bash
Error from server: error when creating "ingress-host.yaml": admission webhook "validating.webhook.kubernetes-policy-controller" denied the request: [
  {
    "id": "ingress-conflict",
    "message": "ingress host conflicts with an existing ingress"
  }

```

## Summary

If you have reached this stage you have succesfully created a policy for your cluster using the `kubernetes-policy-controller`. 
