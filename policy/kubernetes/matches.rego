package k8s
import data.kubernetes

matches[[kind, namespace, name, resource]] {
    resource := kubernetes[kind][namespace][name]
}