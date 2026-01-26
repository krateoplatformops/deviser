-- ============================================================
-- Partition template for k8s_events (daily)
-- ============================================================

CREATE TABLE IF NOT EXISTS {{.PartitionName}}
PARTITION OF k8s_events
FOR VALUES FROM ('{{.StartDate}}') TO ('{{.EndDate}}');

-- ============================================================
-- Deduplication (per partition)
-- ============================================================
CREATE UNIQUE INDEX IF NOT EXISTS uq_{{.PartitionName}}_dedup
ON {{.PartitionName}} (global_uid, resource_version);


-- ============================================================
-- Indexes for queries
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_{{.PartitionName}}_global_uid_created
ON {{.PartitionName}} (global_uid, created_at DESC);

-- Last event for resource
CREATE INDEX IF NOT EXISTS idx_{{.PartitionName}}_resource_latest
ON {{.PartitionName}} (cluster_name, uid, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_{{.PartitionName}}_obj
ON {{.PartitionName}} (
    namespace,
    resource_kind,
    resource_name,
    created_at DESC
);

-- Composition
CREATE INDEX IF NOT EXISTS idx_{{.PartitionName}}_composition
ON {{.PartitionName}} (composition_id, created_at DESC)
WHERE composition_id IS NOT NULL;

-- Event type
CREATE INDEX IF NOT EXISTS idx_{{.PartitionName}}_type
ON {{.PartitionName}} (event_type);

-- Filter on labels
CREATE INDEX IF NOT EXISTS idx_{{.PartitionName}}_labels
ON {{.PartitionName}}
USING GIN ((raw->'involvedObject'->'labels'));