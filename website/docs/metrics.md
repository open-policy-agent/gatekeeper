---
id: metrics
title: Metrics
---

Below are the list of metrics provided by Gatekeeper:

## Constraint

- Name: `gatekeeper_constraints`

    Description: `Current number of known constraints`

    Tags:

    - `enforcement_action`: [`deny`, `dryrun`, `warn`]

    - `status`: [`active`, `error`]

    Aggregation: `LastValue`

## Constraint Template

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

## Webhook

- Name: `gatekeeper_validation_request_count`

    Description: `The number of requests that are routed to validation webhook`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Count`

- Name: `gatekeeper_validation_request_duration_seconds`

    Description: `The validation webhook response time in seconds`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Distribution`

- Name: `gatekeeper_mutation_request_count`

    Description: `The number of requests that are routed to mutation webhook`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Count`

- Name: `gatekeeper_mutation_request_duration_seconds`

    Description: `The mutation webhook response time in seconds`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Distribution`

## Audit

- Name: `gatekeeper_violations`

    Description: `Total number of audited violations`

    Tags:

    - `enforcement_action`: [`deny`, `dryrun`, `warn`]

    Aggregation: `LastValue`

- Name: `gatekeeper_audit_duration_seconds`

    Description: `Latency of audit operation in seconds`

    Aggregation: `Distribution`

- Name: `gatekeeper_audit_last_run_time`

    Description: `Timestamp of last audit run time`

    Aggregation: `LastValue`

## Mutation

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

## Sync

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

## Watch

- Name: `gatekeeper_watch_manager_watched_gvk`

    Description: `Total number of watched GroupVersionKinds`

    Aggregation: `LastValue`

- Name: `gatekeeper_watch_manager_intended_watch_gvk`

    Description: `Total number of GroupVersionKinds with a registered watch intent`

    Aggregation: `LastValue`
