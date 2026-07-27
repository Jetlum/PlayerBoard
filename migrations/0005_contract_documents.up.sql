-- Contract document storage: uploaded PDF/text files for AI analysis.
-- Each document is associated with an athlete and optionally linked to an existing contract.
CREATE TABLE contract_document (
    id           UUID PRIMARY KEY,
    athlete_id   UUID NOT NULL REFERENCES athlete(id),
    contract_id  UUID REFERENCES contract(id),   -- NULL if not linked to an existing contract
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    raw_text     TEXT NOT NULL DEFAULT '',        -- extracted text content for analysis
    file_data    BYTEA,                           -- original file bytes
    label        TEXT NOT NULL DEFAULT '',        -- user-provided label (e.g. "2024 Renewal")
    status       TEXT NOT NULL DEFAULT 'uploaded',-- uploaded | analyzed
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_contract_document_athlete ON contract_document (athlete_id);
