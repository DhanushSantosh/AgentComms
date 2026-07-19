CREATE TABLE IF NOT EXISTS projects (
    project_id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    head_sequence BIGINT NOT NULL DEFAULT 0 CHECK (head_sequence >= 0),
    head_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS actor_keys (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    public_key TEXT NOT NULL,
    valid_from_sequence BIGINT NOT NULL,
    valid_until_sequence BIGINT,
    PRIMARY KEY (project_id, actor_id, valid_from_sequence)
);

CREATE TABLE IF NOT EXISTS events (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    event_id TEXT NOT NULL,
    event_time TIMESTAMPTZ NOT NULL,
    actor_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    entity_id TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL,
    previous_hash TEXT NOT NULL DEFAULT '',
    event_hash TEXT NOT NULL,
    actor_intent_hash TEXT NOT NULL,
    actor_signature TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    receipt JSONB NOT NULL,
    legacy_bytes BYTEA,
    PRIMARY KEY (project_id, sequence),
    UNIQUE (project_id, event_id),
    UNIQUE (project_id, idempotency_key)
) PARTITION BY HASH (project_id);

CREATE TABLE IF NOT EXISTS events_p00 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 0);
CREATE TABLE IF NOT EXISTS events_p01 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 1);
CREATE TABLE IF NOT EXISTS events_p02 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 2);
CREATE TABLE IF NOT EXISTS events_p03 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 3);
CREATE TABLE IF NOT EXISTS events_p04 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 4);
CREATE TABLE IF NOT EXISTS events_p05 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 5);
CREATE TABLE IF NOT EXISTS events_p06 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 6);
CREATE TABLE IF NOT EXISTS events_p07 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 7);
CREATE TABLE IF NOT EXISTS events_p08 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 8);
CREATE TABLE IF NOT EXISTS events_p09 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 9);
CREATE TABLE IF NOT EXISTS events_p10 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 10);
CREATE TABLE IF NOT EXISTS events_p11 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 11);
CREATE TABLE IF NOT EXISTS events_p12 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 12);
CREATE TABLE IF NOT EXISTS events_p13 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 13);
CREATE TABLE IF NOT EXISTS events_p14 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 14);
CREATE TABLE IF NOT EXISTS events_p15 PARTITION OF events FOR VALUES WITH (MODULUS 16, REMAINDER 15);

CREATE INDEX IF NOT EXISTS events_entity_idx ON events (project_id, entity_id, sequence DESC);
CREATE INDEX IF NOT EXISTS events_type_idx ON events (project_id, event_type, sequence DESC);

CREATE TABLE IF NOT EXISTS agents (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, agent_id)
);

CREATE TABLE IF NOT EXISTS tasks (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    task_id TEXT NOT NULL,
    status TEXT NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    worktree TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, task_id)
);

CREATE INDEX IF NOT EXISTS tasks_active_idx ON tasks (project_id, status, lease_until) WHERE archived = FALSE;
CREATE UNIQUE INDEX IF NOT EXISTS tasks_active_worktree_idx ON tasks (project_id, worktree)
    WHERE worktree <> '' AND archived = FALSE AND status NOT IN ('COMPLETED', 'CANCELLED');

CREATE TABLE IF NOT EXISTS task_resources (
    project_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    resource TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (project_id, task_id, resource),
    FOREIGN KEY (project_id, task_id) REFERENCES tasks(project_id, task_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS task_resources_active_idx ON task_resources (project_id, resource) WHERE active = TRUE;

CREATE TABLE IF NOT EXISTS messages (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    message_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    sender_id TEXT NOT NULL,
    status TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, message_id)
);

CREATE TABLE IF NOT EXISTS invocations (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    invocation_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    status TEXT NOT NULL,
    deadline TIMESTAMPTZ,
    claim_until TIMESTAMPTZ,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, invocation_id)
);

CREATE INDEX IF NOT EXISTS invocations_target_status_idx
    ON invocations (project_id, target_id, status, updated_sequence);

CREATE TABLE IF NOT EXISTS invocation_deliveries (
    project_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    invocation_id TEXT NOT NULL,
    runtime_id TEXT NOT NULL DEFAULT '',
    attempt INTEGER NOT NULL,
    status TEXT NOT NULL,
    next_retry_at TIMESTAMPTZ,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, delivery_id),
    FOREIGN KEY (project_id, invocation_id)
        REFERENCES invocations(project_id, invocation_id) ON DELETE CASCADE,
    CHECK (attempt > 0 AND attempt <= 10)
);

CREATE INDEX IF NOT EXISTS invocation_deliveries_retry_idx
    ON invocation_deliveries (project_id, status, next_retry_at);

CREATE TABLE IF NOT EXISTS agent_runtimes (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    runtime_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    connector TEXT NOT NULL,
    status TEXT NOT NULL,
    health TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, runtime_id)
);

CREATE INDEX IF NOT EXISTS agent_runtimes_agent_status_idx
    ON agent_runtimes (project_id, agent_id, status);

CREATE TABLE IF NOT EXISTS invocation_policies (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,
    mode TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, agent_id)
);

CREATE TABLE IF NOT EXISTS message_recipients (
    project_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    status TEXT NOT NULL,
    responded_at TIMESTAMPTZ,
    PRIMARY KEY (project_id, message_id, principal_id),
    FOREIGN KEY (project_id, message_id) REFERENCES messages(project_id, message_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS message_recipient_inbox_idx
    ON message_recipients (project_id, principal_id, status);

CREATE TABLE IF NOT EXISTS approvals (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    approval_id TEXT NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, approval_id)
);

CREATE INDEX IF NOT EXISTS approvals_action_idx ON approvals (project_id, action, status);

CREATE TABLE IF NOT EXISTS decisions (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    decision_id TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, decision_id)
);

CREATE TABLE IF NOT EXISTS documents (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    status TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, document_id)
);

CREATE TABLE IF NOT EXISTS artifacts (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    sha256 TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, sha256)
);

CREATE TABLE IF NOT EXISTS environment_entries (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    entry_key TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, entry_key)
);

CREATE TABLE IF NOT EXISTS sessions (
    project_id TEXT NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_sequence BIGINT NOT NULL,
    PRIMARY KEY (project_id, session_id)
);

CREATE TABLE IF NOT EXISTS outbox (
    project_id TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    event JSONB NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,
    PRIMARY KEY (project_id, sequence),
    FOREIGN KEY (project_id, sequence) REFERENCES events(project_id, sequence) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox (next_attempt_at)
    WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS legacy_imports (
    project_id TEXT PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    legacy_head_hash TEXT NOT NULL,
    legacy_git_commit TEXT NOT NULL,
    imported_sequence BIGINT NOT NULL DEFAULT 0,
    expected_events BIGINT NOT NULL,
    projection_hash TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    receipt JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE legacy_imports ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE legacy_imports ADD COLUMN IF NOT EXISTS source_public_key TEXT NOT NULL DEFAULT '';
ALTER TABLE legacy_imports ADD COLUMN IF NOT EXISTS source_head_hash TEXT NOT NULL DEFAULT '';
