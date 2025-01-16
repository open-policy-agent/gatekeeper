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
| `--external-data-provider-response-cache-ttl`  | `3 * time.Minute`                               |             |
| `--audit-interval`                             | `60`                                            |             |
| `--constraint-violations-limit`                | `20`                                            |             |
| `--audit-chunk-size`                           | `500`                                           |             |
| `--audit-from-cache`                           | `false`                                         |             |
| `--emit-audit-events`                          | `false`                                         |             |
| `--audit-events-involved-namespace`            | `false`                                         |             |
| `--audit-match-kind-only`                      | `false`                                         |             |
| `--api-cache-dir`                              | `"/tmp/audit"`                                  |             |
| `--audit-connection`                           | `"audit-connection"`                            |             |
| `--audit-channel`                              | `"audit-channel"`                               |             |
| `--log-stats-audit`                            | `false`                                         |             |
| `--default-create-vap-binding-for-constraints` | `false`                                         |             |
| `--default-create-vap-for-templates`           | `false`                                         |             |
| `--default-wait-for-vapb-generation`           | `30`                                            |             |
| `--debug-use-fake-pod`                         | `false`                                         |             |
| `--enable-pub-sub`                             | `false`                                         |             |
| `--enable-generator-resource-expansion`        | `true`                                          |             |
| `--enable-external-data`                       | `true`                                          |             |
| `--otlp-endpoint`                              | `""`                                            |             |
| `--otlp-metric-interval`                       | `10 * time.Second`                              |             |
| `--prometheus-port`                            | `8888`                                          |             |
| `--stackdriver-only-when-available`            | `false`                                         |             |
| `--stackdriver-metric-interval`                | `10 * time.Second`                              |             |
| `--metrics-backend`                            | `prometheus`                                    |             |
| `--enable-mutation`                            | `false`                                         |             |
| `--log-mutations`                              | `false`                                         |             |
| `--mutation-annotations`                       | `false`                                         |             |
| `--operation`                                  | None                                            |             |
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
| `--exempt-namespace`                           | None                                            |             |
| `--exempt-namespace-prefix`                    | None                                            |             |
| `--exempt-namespace-suffix`                    | None                                            |             |
| `--max-serving-threads`                        | `-1`                                            |             |
