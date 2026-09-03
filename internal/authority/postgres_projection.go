package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// Postgres projection state: rebuilding model.State from the per-table
// projection rows (loadState / loadProjection) and writing State deltas
// back (persist*Changes). Split out of postgres.go.

func cloneState(state model.State) model.State {
	raw, _ := json.Marshal(state)
	var clone model.State
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func loadState(ctx context.Context, tx *sql.Tx, projectID string) (model.State, error) {
	state := model.EmptyState()
	loaders := []func() error{
		func() error {
			return loadProjection(ctx, tx, "agents", "agent_id", projectID, func(id string, raw []byte) error {
				var v model.Agent
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Agents[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "tasks", "task_id", projectID, func(id string, raw []byte) error {
				var v model.Task
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Tasks[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "messages", "message_id", projectID, func(id string, raw []byte) error {
				var v model.Message
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Messages[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "invocations", "invocation_id", projectID, func(id string, raw []byte) error {
				var value model.Invocation
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.Invocations[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "invocation_deliveries", "delivery_id", projectID, func(id string, raw []byte) error {
				var value model.InvocationDelivery
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.InvocationDeliveries[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "agent_runtimes", "runtime_id", projectID, func(id string, raw []byte) error {
				var value model.AgentRuntime
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.AgentRuntimes[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "invocation_policies", "agent_id", projectID, func(id string, raw []byte) error {
				var value model.InvocationPolicy
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.InvocationPolicies[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "approvals", "approval_id", projectID, func(id string, raw []byte) error {
				var v model.Approval
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Approvals[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "documents", "document_id", projectID, func(id string, raw []byte) error {
				var v model.Document
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Documents[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "artifacts", "sha256", projectID, func(id string, raw []byte) error {
				var v model.Artifact
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Artifacts[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "environment_entries", "entry_key", projectID, func(id string, raw []byte) error {
				var v model.EnvEntry
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Env[id] = v
				return nil
			})
		},
	}
	for _, load := range loaders {
		if err := load(); err != nil {
			return state, unavailable(err)
		}
	}
	var settingsRaw []byte
	err := tx.QueryRowContext(ctx, "SELECT state FROM project_settings WHERE project_id=$1", projectID).Scan(&settingsRaw)
	if err == nil {
		if err = json.Unmarshal(settingsRaw, &state.ProjectSettings); err != nil {
			return state, unavailable(err)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, unavailable(err)
	}
	return state, nil
}

func loadProjection(ctx context.Context, tx *sql.Tx, table, idColumn, projectID string, accept func(string, []byte) error) error {
	allowed := map[string]bool{
		"agents.agent_id": true, "tasks.task_id": true, "messages.message_id": true,
		"invocations.invocation_id": true, "invocation_deliveries.delivery_id": true,
		"agent_runtimes.runtime_id": true, "invocation_policies.agent_id": true,
		"approvals.approval_id": true,
		"documents.document_id": true, "artifacts.sha256": true,
		"environment_entries.entry_key": true,
	}
	if !allowed[table+"."+idColumn] {
		return errors.New("invalid projection query")
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT %s, state FROM %s WHERE project_id=$1", idColumn, table), projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return err
		}
		if err = accept(id, raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

func persistProjectionChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after model.State) error {
	if !reflect.DeepEqual(before.ProjectSettings, after.ProjectSettings) {
		raw, err := json.Marshal(after.ProjectSettings)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_settings (project_id,state,updated_sequence)
			VALUES ($1,$2,$3) ON CONFLICT (project_id) DO UPDATE SET
			state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`, projectID, raw, sequence); err != nil {
			return err
		}
	}
	for id, value := range after.Agents {
		if reflect.DeepEqual(before.Agents[id], value) {
			continue
		}
		if err := upsertAgent(ctx, tx, projectID, sequence, value); err != nil {
			return err
		}
	}
	for id := range before.Agents {
		if _, exists := after.Agents[id]; exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE project_id=$1 AND agent_id=$2`, projectID, id); err != nil {
			return err
		}
	}
	for id, value := range after.Tasks {
		if reflect.DeepEqual(before.Tasks[id], value) {
			continue
		}
		if err := upsertTask(ctx, tx, projectID, sequence, value); err != nil {
			return err
		}
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "messages", "message_id", before.Messages, after.Messages); err != nil {
		return err
	}
	if err := persistInvocationChanges(ctx, tx, projectID, sequence, before.Invocations, after.Invocations); err != nil {
		return err
	}
	if err := persistInvocationDeliveryChanges(ctx, tx, projectID, sequence, before.InvocationDeliveries, after.InvocationDeliveries); err != nil {
		return err
	}
	if err := persistRuntimeChanges(ctx, tx, projectID, sequence, before.AgentRuntimes, after.AgentRuntimes); err != nil {
		return err
	}
	if err := persistInvocationPolicyChanges(ctx, tx, projectID, sequence, before.InvocationPolicies, after.InvocationPolicies); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "approvals", "approval_id", before.Approvals, after.Approvals); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "documents", "document_id", before.Documents, after.Documents); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "artifacts", "sha256", before.Artifacts, after.Artifacts); err != nil {
		return err
	}
	return persistSimpleChanges(ctx, tx, projectID, sequence, "environment_entries", "entry_key", before.Env, after.Env)
}

func persistRuntimeChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.AgentRuntime) error {
	for id, runtime := range after {
		if reflect.DeepEqual(before[id], runtime) {
			continue
		}
		raw, err := json.Marshal(runtime)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runtimes
			(project_id,runtime_id,agent_id,runtime_kind,connector,host_id,status,health,last_seen_at,state,updated_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (project_id,runtime_id) DO UPDATE SET agent_id=EXCLUDED.agent_id,
			runtime_kind=EXCLUDED.runtime_kind,connector=EXCLUDED.connector,host_id=EXCLUDED.host_id,
			status=EXCLUDED.status,health=EXCLUDED.health,
			last_seen_at=EXCLUDED.last_seen_at,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, runtime.AgentID, runtime.Kind, runtime.Connector, runtime.HostID,
			runtime.Status, runtime.Health, nullableTime(runtime.LastSeenAt), raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistInvocationPolicyChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.InvocationPolicy) error {
	for id, policy := range after {
		if reflect.DeepEqual(before[id], policy) {
			continue
		}
		raw, err := json.Marshal(policy)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO invocation_policies
			(project_id,agent_id,mode,state,updated_sequence) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (project_id,agent_id) DO UPDATE SET mode=EXCLUDED.mode,
			state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, policy.Mode, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistInvocationChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.Invocation) error {
	for id, invocation := range after {
		if reflect.DeepEqual(before[id], invocation) {
			continue
		}
		raw, err := json.Marshal(invocation)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO invocations
			(project_id,invocation_id,target_id,requested_by,consumer_mode,preferred_runtime_id,status,deadline,claim_until,state,updated_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (project_id,invocation_id) DO UPDATE SET target_id=EXCLUDED.target_id,
			requested_by=EXCLUDED.requested_by,consumer_mode=EXCLUDED.consumer_mode,
			preferred_runtime_id=EXCLUDED.preferred_runtime_id,status=EXCLUDED.status,
			deadline=EXCLUDED.deadline,claim_until=EXCLUDED.claim_until,
			state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, invocation.Target, invocation.RequestedBy, invocation.ConsumerMode,
			invocation.PreferredRuntimeID, invocation.Status, invocation.Deadline,
			invocation.ClaimUntil, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistInvocationDeliveryChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.InvocationDelivery) error {
	for id, delivery := range after {
		if reflect.DeepEqual(before[id], delivery) {
			continue
		}
		raw, err := json.Marshal(delivery)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO invocation_deliveries
			(project_id,delivery_id,invocation_id,runtime_id,transport,attempt,status,next_retry_at,state,updated_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (project_id,delivery_id) DO UPDATE SET invocation_id=EXCLUDED.invocation_id,
			runtime_id=EXCLUDED.runtime_id,transport=EXCLUDED.transport,
			attempt=EXCLUDED.attempt,status=EXCLUDED.status,
			next_retry_at=EXCLUDED.next_retry_at,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, delivery.InvocationID, delivery.RuntimeID, delivery.Transport,
			delivery.Attempt, delivery.Status, delivery.NextRetryAt, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func upsertAgent(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, agent model.Agent) error {
	raw, _ := json.Marshal(agent)
	_, err := tx.ExecContext(ctx, `INSERT INTO agents (project_id,agent_id,state,updated_sequence) VALUES ($1,$2,$3,$4)
		ON CONFLICT (project_id,agent_id) DO UPDATE SET state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
		projectID, agent.ID, raw, sequence)
	if err != nil {
		return err
	}
	if agent.PublicKey != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO actor_keys (project_id,actor_id,fingerprint,public_key,valid_from_sequence)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			projectID, agent.ID, identity.Fingerprint(agent.PublicKey), agent.PublicKey, sequence)
	}
	return err
}

func upsertTask(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, task model.Task) error {
	raw, _ := json.Marshal(task)
	_, err := tx.ExecContext(ctx, `INSERT INTO tasks
		(project_id,task_id,status,owner_id,worktree,lease_until,archived,state,updated_sequence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (project_id,task_id) DO UPDATE SET status=EXCLUDED.status,owner_id=EXCLUDED.owner_id,
		worktree=EXCLUDED.worktree,lease_until=EXCLUDED.lease_until,archived=EXCLUDED.archived,
		state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
		projectID, task.ID, task.Status, task.Owner, task.Worktree, nullableTime(task.LeaseUntil), task.Archived, raw, sequence)
	if err != nil {
		return err
	}
	active := !task.Archived && task.Status != "COMPLETED" && task.Status != "CANCELLED"
	if _, err = tx.ExecContext(ctx, `UPDATE task_resources SET active=$3 WHERE project_id=$1 AND task_id=$2`,
		projectID, task.ID, active); err != nil {
		return err
	}
	for _, resource := range task.Resources {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_resources (project_id,task_id,resource,active)
			VALUES ($1,$2,$3,$4) ON CONFLICT (project_id,task_id,resource) DO UPDATE SET active=EXCLUDED.active`,
			projectID, task.ID, resource, active); err != nil {
			return err
		}
	}
	return nil
}

func persistSimpleChanges[V any](ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, table, idColumn string, before, after map[string]V) error {
	allowed := map[string]bool{
		"messages.message_id": true, "approvals.approval_id": true,
		"documents.document_id": true,
		"artifacts.sha256":      true, "environment_entries.entry_key": true,
	}
	if !allowed[table+"."+idColumn] {
		return errors.New("invalid projection update")
	}
	for id, value := range after {
		if reflect.DeepEqual(before[id], value) {
			continue
		}
		raw, _ := json.Marshal(value)
		query := fmt.Sprintf(`INSERT INTO %s (project_id,%s,state,updated_sequence) VALUES ($1,$2,$3,$4)
			ON CONFLICT (project_id,%s) DO UPDATE SET state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			table, idColumn, idColumn)
		if table == "messages" {
			message := any(value).(model.Message)
			query = `INSERT INTO messages (project_id,message_id,kind,sender_id,status,task_id,state,updated_sequence)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (project_id,message_id) DO UPDATE SET
				kind=EXCLUDED.kind,sender_id=EXCLUDED.sender_id,status=EXCLUDED.status,task_id=EXCLUDED.task_id,
				state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`
			if _, err := tx.ExecContext(ctx, query, projectID, id, message.Kind, message.From, message.Status, message.TaskID, raw, sequence); err != nil {
				return err
			}
			for _, recipient := range message.Recipients {
				if _, err := tx.ExecContext(ctx, `INSERT INTO message_recipients
					(project_id,message_id,principal_id,status,responded_at) VALUES ($1,$2,$3,$4,$5)
					ON CONFLICT (project_id,message_id,principal_id) DO UPDATE SET status=EXCLUDED.status,responded_at=EXCLUDED.responded_at`,
					projectID, id, recipient.Principal, recipient.Status, recipient.At); err != nil {
					return err
				}
			}
			continue
		}
		if table == "approvals" {
			approval := any(value).(model.Approval)
			query = `INSERT INTO approvals (project_id,approval_id,action,status,state,updated_sequence)
				VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (project_id,approval_id) DO UPDATE SET
				action=EXCLUDED.action,status=EXCLUDED.status,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`
			if _, err := tx.ExecContext(ctx, query, projectID, id, approval.Action, approval.Status, raw, sequence); err != nil {
				return err
			}
			continue
		}
		if table == "documents" {
			document := any(value).(model.Document)
			query = `INSERT INTO documents (project_id,document_id,status,state,updated_sequence)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT (project_id,document_id) DO UPDATE SET
				status=EXCLUDED.status,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`
			if _, err := tx.ExecContext(ctx, query, projectID, id, document.Status, raw, sequence); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, query, projectID, id, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}
