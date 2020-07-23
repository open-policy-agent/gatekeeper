# List of metrics provided by Gatekeeper

## Constraint

- Name: `constraints`

    Description: `Current number of known constraints`

    Tags:

    - `enforcement_action`: [`deny`, `dryrun`]

    - `status`: [`active`, `error`]

    Aggregation: `LastValue`

## Constraint Template

- Name: `constraint_templates`

    Description: `Number of observed constraint templates`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `LastValue`

- Name: `constraint_template_ingestion_count`

    Description: `Total number of constraint template ingestion actions`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Count`

- Name: `constraint_template_ingestion_duration_seconds`

    Description: `Distribution of how long it took to ingest a constraint template in seconds`

    Tags:

    - `status`: [`active`, `error`]

    Aggregation: `Distribution`

## Webhook

- Name: `request_count`

    Description: `The number of requests that are routed to webhook`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Count`

- Name: `request_duration_seconds`

    Description: `The response time in seconds`

    Tags:

    - `admission_status`: [`allow`, `deny`]

    Aggregation: `Distribution`

## Audit

- Name: `violations`

    Description: `Total number of violations per constraint`

    Tags:

    - `enforcement_action`: [`deny`, `dryrun`]

    Aggregation: `LastValue`

- Name: `audit_duration_seconds`

    Description: `Latency of audit operation in seconds`

    Aggregation: `Distribution`

- Name: `audit_last_run_time`

    Description: `Timestamp of last audit run time`

    Aggregation: `LastValue`

## Sync

- Name: `sync`

    Description: `Total number of resources of each kind being cached`

    Tags:

    - `status`: [`active`, `error`]

    - `kind` (examples, `pod`, `namespace`, ...)

    Aggregation: `LastValue`

- Name: `sync_duration_seconds`

    Description: `Latency of sync operation in seconds`

    Aggregation: `Distribution`

- Name: `sync_last_run_time`

    Description: `Timestamp of last sync operation`

    Aggregation: `LastValue`

## Watch

- Name: `watch_manager_watched_gvk`

    Description: `Total number of watched GroupVersionKinds`

    Aggregation: `LastValue`

- Name: `watch_manager_intended_watch_gvk`

    Description: `Total number of GroupVersionKinds with a registered watch intent`

    Aggregation: `LastValue`
