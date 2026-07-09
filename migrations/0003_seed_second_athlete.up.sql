-- Second demo athlete so the ClubBoard/PlayerBoard split is provable: recording an
-- appearance for one player must only notify that player's board, not the other's.
-- Player "Rafael Silva", Palmeiras -> Corinthians, appearances bonus (target 15, tranche 5).
INSERT INTO athlete (id, handle, display_name) VALUES
    ('66666666-6666-6666-6666-666666666666', 'rafael', 'Rafael Silva')
ON CONFLICT (id) DO NOTHING;

INSERT INTO contract (id, athlete_id, club_from, club_to, currency, fixed_amount, salary, status) VALUES
    ('77777777-7777-7777-7777-777777777777',
     '66666666-6666-6666-6666-666666666666',
     'Palmeiras', 'Corinthians', 'BRL', 6000000, 7200000, 'active')
ON CONFLICT (id) DO NOTHING;

INSERT INTO clause (id, contract_id, kind, params) VALUES
    ('88888888-8888-8888-8888-888888888888',
     '77777777-7777-7777-7777-777777777777',
     'performance',
     '{"metric":"appearances","target":15,"tranche":5,"max":-1,"amount":1800000,"currency":"BRL"}'::jsonb)
ON CONFLICT (id) DO NOTHING;

-- Starts at 13/15 (also "hot", 2 away) so both seeded players are demo-ready in one click.
INSERT INTO milestone (id, athlete_id, clause_id, metric, target, tranche, max_repeats, amount, currency, progress, state) VALUES
    ('99999999-9999-9999-9999-999999999999',
     '66666666-6666-6666-6666-666666666666',
     '88888888-8888-8888-8888-888888888888',
     'appearances', 15, 5, -1, 1800000, 'BRL', 13, 'hot')
ON CONFLICT (clause_id) DO NOTHING;

INSERT INTO performance_stat (id, athlete_id, metric, value, source_event_id) VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
     '66666666-6666-6666-6666-666666666666',
     'appearances', 13, 'seed-rafael-appearances-13')
ON CONFLICT (source_event_id) DO NOTHING;
