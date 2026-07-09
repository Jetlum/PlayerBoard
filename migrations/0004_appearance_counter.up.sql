-- Fixes a real race condition: the ClubBoard "record appearance" action used to compute the
-- next appearance number by READING milestone.progress and adding 1 in application code. Two
-- rapid clicks (or two club operators acting at once) could both read the same progress before
-- either webhook had been processed by the async worker, and both would send the same
-- duplicate value — silently dropped downstream as a no-op advance.
--
-- appearance_counter is the club's own ground truth for "how many appearances have been
-- recorded so far", incremented with a single atomic UPSERT statement (ON CONFLICT DO UPDATE),
-- so Postgres itself serializes concurrent increments. It is intentionally decoupled from
-- milestone.progress, which remains the worker's own (eventually-consistent) view.
CREATE TABLE appearance_counter (
    athlete_id UUID NOT NULL REFERENCES athlete(id),
    metric     TEXT NOT NULL,
    value      BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (athlete_id, metric)
);

-- Seed to match the existing seeded milestone progress (0002/0003) so the first click after
-- this migration continues the sequence rather than restarting it.
INSERT INTO appearance_counter (athlete_id, metric, value) VALUES
    ('11111111-1111-1111-1111-111111111111', 'appearances', 18),
    ('66666666-6666-6666-6666-666666666666', 'appearances', 13)
ON CONFLICT (athlete_id, metric) DO NOTHING;
