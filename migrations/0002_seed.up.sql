-- Demo seed: player "Everton", Flamengo -> Benfica, appearances bonus (target 20, tranche 10).
-- Fixed IDs so the dev token minter and demo curl scripts can reference them.
INSERT INTO athlete (id, handle, display_name) VALUES
    ('11111111-1111-1111-1111-111111111111', 'everton', 'Everton')
ON CONFLICT (id) DO NOTHING;

INSERT INTO contract (id, athlete_id, club_from, club_to, currency, fixed_amount, salary, status) VALUES
    ('22222222-2222-2222-2222-222222222222',
     '11111111-1111-1111-1111-111111111111',
     'Flamengo', 'Benfica', 'BRL', 8000000, 9500000, 'active')
ON CONFLICT (id) DO NOTHING;

INSERT INTO clause (id, contract_id, kind, params) VALUES
    ('33333333-3333-3333-3333-333333333333',
     '22222222-2222-2222-2222-222222222222',
     'performance',
     '{"metric":"appearances","target":20,"tranche":10,"max":-1,"amount":2500000,"currency":"BRL"}'::jsonb)
ON CONFLICT (id) DO NOTHING;

-- Player starts at 18 appearances; the demo webhooks push #19 then #20 (crosses the target=20 boundary).
INSERT INTO milestone (id, athlete_id, clause_id, metric, target, tranche, max_repeats, amount, currency, progress, state) VALUES
    ('44444444-4444-4444-4444-444444444444',
     '11111111-1111-1111-1111-111111111111',
     '33333333-3333-3333-3333-333333333333',
     'appearances', 20, 10, -1, 2500000, 'BRL', 18, 'hot')
ON CONFLICT (clause_id) DO NOTHING;

INSERT INTO performance_stat (id, athlete_id, metric, value, source_event_id) VALUES
    ('55555555-5555-5555-5555-555555555555',
     '11111111-1111-1111-1111-111111111111',
     'appearances', 18, 'seed-appearances-18')
ON CONFLICT (source_event_id) DO NOTHING;
