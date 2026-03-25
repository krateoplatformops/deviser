-- ============================================================
-- Table: k8s_events (partitioned by created_at)
-- ============================================================

CREATE TABLE IF NOT EXISTS k8s_events (
    created_at       TIMESTAMPTZ NOT NULL,

    -- Identità cluster / resource
    cluster_name     TEXT NOT NULL,
    uid              TEXT NOT NULL,
    global_uid       TEXT NOT NULL, -- cluster_name:uid

    namespace        TEXT NOT NULL,
    resource_kind    TEXT NOT NULL,
    resource_name    TEXT NOT NULL,
    involved_object_uid TEXT NULL,

    -- Event data
    event_type       TEXT NOT NULL,
    reason           TEXT NULL,
    message          TEXT NULL,

    -- Krateo / extensions
    composition_id   UUID NULL,

    -- Raw event
    raw              JSONB NOT NULL,

    -- Dedup
    resource_version TEXT NOT NULL
)
PARTITION BY RANGE (created_at);


-- ============================================================
-- Trigger function: notify on new event
-- ============================================================

CREATE OR REPLACE FUNCTION notify_new_event()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('events', NEW.global_uid);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS k8s_events_notify ON k8s_events;

CREATE TRIGGER k8s_events_notify
AFTER INSERT ON k8s_events
FOR EACH ROW
EXECUTE FUNCTION notify_new_event();
