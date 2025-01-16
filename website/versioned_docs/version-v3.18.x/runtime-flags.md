---
id: runtime-flags
title: Runtime Flags
---

## Runtime Flags

| Flag                                           | Default Value                                   | Description |
|------------------------------------------------|-------------------------------------------------|-------------|
| `--log-file`                                   | `""`                                            |             |
| `--log-level`                                  | `"INFO"`                                        |             |
| `--log-level-key`                              | `"level"`                                       |             |
| `--log-level-encoder`                          | `"lower"`                                       |             |
| `--health-addr`                                | `":9090"`                                       |             |
| `--metrics-addr`                               | `"0"`                                           |             |
| `--port`                                       | `443`                                           |             |
| `--host`                                       | `""`                                            |             |
| `--cert-dir`                                   | `"/certs"`                                      |             |
| `--disable-cert-rotation`                      | `false`                                         |             |
| `--enable-pprof`                               | `false`                                         |             |
| `--pprof-port`                                 | `6060`                                          |             |
| `--cert-service-name`                          | `"gatekeeper-webhook-service"`                  |             |
| `--enable-tls-healthcheck`                     | `false`                                         |             |
| `--enable-k8s-native-validation`               | `true`                                          |             |
| `--external-data-provider-response-cache-ttl`  | `3*time.Minute`                                 |             |
| `--audit-interval`                             | ``                                              |             |
| `--constraint-violations-limit`                | ``                                              |             |
| `--audit-chunk-size`                           | ``                                              |             |
| `--audit-from-cache`                           | `false`                                         |             |
| `--emit-audit-events`                          | `false`                                         |             |
| `--audit-events-involved-namespace`            | `false`                                         |             |
| `--audit-match-kind-only`                      | `false`                                         |             |
| `--api-cache-dir`                              | ``                                              |             |
| `--audit-connection`                           | ``                                              |             |
| `--audit-channel`                              | ``                                              |             |
| `--log-stats-audit`                            | `false`                                         |             |
| `--default-create-vap-binding-for-constraints` | `false`                                         |             |
| `--default-create-vap-for-templates`           | `false`                                         |             |
| `--default-wait-for-vapb-generation`           | `30`                                            |             |
| `--debug-use-fake-pod`                         | `false`                                         |             |
| `--enable-pub-sub`                             | `false`                                         |             |
| `--enable-generator-resource-expansion`        | `true`                                          |             |
| `--enable-external-data`                       | `true`                                          |             |
| `--otlp-endpoint`                              | `""`                                            |             |
| `--otlp-metric-interval`                       | ``                                              |             |
| `--prometheus-port`                            | `8888`                                          |             |
| `--stackdriver-only-when-available`            | `false`                                         |             |
| `--stackdriver-metric-interval`                | ``                                              |             |
| `--metrics-backend`                            | `prometheus`                                    |             |
| `--enable-mutation`                            | `false`                                         |             |
| `--log-mutations`                              | `false`                                         |             |
| `--mutation-annotations`                       | `false`                                         |             |
| `--operation`                                  | ``                                              |             |
| `--readiness-retries`                          | `0`                                             |             |
| `--disable-enforcementaction-validation`       | `false`                                         |             |
| `--log-denies`                                 | `false`                                         |             |
| `--emit-admission-events`                      | `false`                                         |             |
| `--admission-events-involved-namespace`        | `false`                                         |             |
| `--log-stats-admission`                        | `false`                                         |             |
| `--validating-webhook-configuration-name`      | `"gatekeeper-validating-webhook-configuration"` |             |
| `--mutating-webhook-configuration-name`        | `"gatekeeper-mutating-webhook-configuration"`   |             |
| `--tls-min-version`                            | `"1.3"`                                         |             |
| `--client-ca-name`                             | `""`                                            |             |
| `--client-cn-name`                             | `"kube-apiserver"`                              |             |
| `--exempt-namespace`                           | ``                                              |             |
| `--exempt-namespace-prefix`                    | ``                                              |             |
| `--exempt-namespace-suffix`                    | ``                                              |             |
| `--max-serving-threads`                        | `-1`                                            |             |
