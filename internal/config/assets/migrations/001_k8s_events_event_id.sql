CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE k8s_events
ADD COLUMN IF NOT EXISTS event_id UUID DEFAULT gen_random_uuid();

UPDATE k8s_events
SET event_id = gen_random_uuid()
WHERE event_id IS NULL;

ALTER TABLE k8s_events
ALTER COLUMN event_id SET NOT NULL;

CREATE OR REPLACE FUNCTION notify_new_event()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify(
        'events',
        json_build_object(
            'event_id', NEW.event_id,
            'global_uid', NEW.global_uid
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
