---
id: metrics
title: Metrics & Observability
---
## Observability

This section covers how to gather more detailed statistics about Gatekeeper's query performance. This can be helpful in diagnosing situations such as identifying a constraint template with a long execution time. Statistics are written to Gatekeeper's stdout logs.

### Logging Constraint Execution Stats

- set `--log-stats-audit`. This flag enables logging the stats for the audit process.

- set `--log-stats-admission`. This flag enables logging the stats for the admission review process.

#### Example Log Line

To see how long it takes to review a constraint kind at admission time, enable the `--log-stats-admission` flag and watch the logs for a constraint kind `K8sRequiredLabels`,  for example:

```json
{
  "level": "info",
  "ts": 1683692576.9093642,
  "logger": "webhook",
  "msg": "admission review request stats",
  "hookType": "validation",
  "process": "admission",
  "event_type": "review_response_stats",
  "resource_group": "",
  "resource_api_version": "v1",
  "resource_kind": "Namespace",
  "resource_namespace": "",
  "request_username": "kubernetes-admin",
  "execution_stats": [
    {
      "scope": "template",
      "statsFor": "K8sRequiredLabels",
      "stats": [
        {
          "name": "templateRunTimeNS",
          "value": 762561,
          "source": {
            "type": "engine",
            "value": "Rego"
          },
          "description": "the number of nanoseconds it took to evaluate all constraints for a template"
        },
        {
          "name": "constraintCount",
          "value": 1,
          "source": {
            "type": "engine",
            "value": "Rego"
          },
          "description": "the number of constraints that were evaluated for the given constraint kind"
        }
      ],
      "labels": [
        {
          "name": "TracingEnabled",
          "value": false
        },
        {
          "name": "PrintEnabled",
          "value": false
        },
        {
          "name": "target",
          "value": "admission.k8s.gatekeeper.sh"
        }
      ]
    }
  ]
}
```

In the excerpt above, notice `templateRunTimeNS` and `constraintCount`. The former indicates the time it takes to evaluate the number of constraints of kind `K8sRequiredLabels`, while the latter surfaces how many such constraints were evaluated for this template. Labels provide additional information about the execution environemnt setup, like whether tracing was enabled (`TraceEnabled`).

#### Caveats

The additional log volume from enabling the stats logging can be quite high.
## Metrics

> If you are using a Prometheus client library, for counter metrics, the _total suffix is recommended and sometimes automatically appended by client libraries to indicate that the metric represents a cumulative total.

Below are the list of metrics provided by Gatekeeper:

### Constraint

- Name: `gatekeeper_constraints`

    Description: `Current number of known constraints`

    Tags:

    - `enforcement_action`: [`deny`, `dryrun`, `warn`]

    - `status`: [`active`, `error`]

    Aggregation: `LastValue`

### Constraint Template

- Name: `gatekeeper_constraint_templates`

    Description: `Number of observed constraint templates`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `LastValue`

- Name: `gatekeeper_constraint_template_ingestion_count`

    Description: `Total number of constraint template ingestion actions`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Count`

- Name: `gatekeeper_constraint_template_ingestion_duration_seconds`

    Description: `Distribution of how long it took to ingest a constraint template in seconds`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Distribution`

### Expansion Template

- Name: `gatekeeper_expansion_templates`

  Description: `Number of observed expansion templates`

  Tags:

  - `status`: [`active`, `error`]

  Aggregation: `LastValue`

### Webhook

- Name: `gatekeeper_validation_request_count`

    Description: `The number of requests that are routed to validation webhook`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    - `admission_dryrun`: [`true`, `false`]

    Aggregation: `Count`

- Name: `gatekeeper_validation_request_duration_seconds`

    Description: `The validation webhook response time in seconds`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Distribution`

- Name: `gatekeeper_mutation_request_count `

    Description: `The number of requests that are routed to mutation webhook`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Count`

- Name: `gatekeeper_mutation_request_duration_seconds`

    Description: `The mutation webhook response time in seconds`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Distribution`

### Audit

- Name: `gatekeeper_violations`

    Description: `Total number of audited violations`

    Tags:

    - `enforcement_action`: [`deny`, `dryrun`, `warn`]

    Aggregation: `LastValue`

- Name: `gatekeeper_audit_duration_seconds`

    Description: `Latency of audit operation in seconds`

    Aggregation: `Distribution`

- Name: `gatekeeper_audit_last_run_time`

    Description: `Timestamp of last audit run starting time`

    Aggregation: `LastValue`

- Name: `gatekeeper_audit_last_run_end_time`

    Description: `Timestamp of last audit run ending time`

    Aggregation: `LastValue`

### Mutation

- Name: `gatekeeper_mutator_ingestion_count`

    Description: `Total number of Mutator ingestion actions`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Count`

- Name: `gatekeeper_mutator_ingestion_duration_seconds`

    Description: `The distribution of Mutator ingestion durations`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Distribution`

- Name: `gatekeeper_mutators`

    Description: `The current number of Mutator objects`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Count`

- Name: `gatekeeper_mutator_conflicting_count`

    Description: `The current number of conflicting Mutator objects`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Count`

### Sync

- Name: `gatekeeper_sync`

    Description: `Total number of resources of each kind being cached`

    Tags:

    - `status`: [`active`, `error`]

    - `kind` (examples, `pod`, `namespace`, ...)

    Aggregation: `LastValue`

- Name: `gatekeeper_sync_duration_seconds`

    Description: `Latency of sync operation in seconds`

    Aggregation: `Distribution`

- Name: `gatekeeper_sync_last_run_time`

    Description: `Timestamp of last sync operation`

    Aggregation: `LastValue`

### Watch

- Name: `gatekeeper_watch_manager_watched_gvk`

    Description: `Total number of watched GroupVersionKinds`

    Aggregation: `LastValue`

- Name: `gatekeeper_watch_manager_intended_watch_gvk`

    Description: `Total number of GroupVersionKinds with a registered watch intent`

    Aggregation: `LastValue`
