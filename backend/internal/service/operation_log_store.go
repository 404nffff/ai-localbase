package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-localbase/internal/model"

	_ "modernc.org/sqlite"
)

const (
	defaultOperationLogLimit     = 20
	maxOperationLogLimit         = 100
	defaultOperationLogRetention = 1000
)

type OperationLogStore interface {
	Append(entry model.OperationLogEntry) error
	List(query model.OperationLogListQuery) (model.OperationLogListResponse, error)
	Close() error
}

type SQLiteOperationLogStore struct {
	db        *sql.DB
	retention int
}

func NewSQLiteOperationLogStore(path string) (*SQLiteOperationLogStore, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil, fmt.Errorf("sqlite operation log path is required")
	}

	if err := os.MkdirAll(filepath.Dir(trimmedPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite operation log directory: %w", err)
	}

	db, err := sql.Open("sqlite", trimmedPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite operation log database: %w", err)
	}

	store := &SQLiteOperationLogStore{db: db, retention: defaultOperationLogRetention}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteOperationLogStore) init() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite operation log store is nil")
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS operation_logs (
			id TEXT PRIMARY KEY,
			correlation_id TEXT NOT NULL,
			operation TEXT NOT NULL,
			source TEXT NOT NULL,
			status TEXT NOT NULL,
			knowledge_base_id TEXT NOT NULL DEFAULT '',
			knowledge_base_name TEXT NOT NULL DEFAULT '',
			document_id TEXT NOT NULL DEFAULT '',
			document_name TEXT NOT NULL DEFAULT '',
			file_size INTEGER NOT NULL DEFAULT 0,
			size_label TEXT NOT NULL DEFAULT '',
			stage TEXT NOT NULL DEFAULT '',
			index_status TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			metadata TEXT NOT NULL DEFAULT '{}',
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_operation_logs_created_at ON operation_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_operation_logs_kb_created_at ON operation_logs(knowledge_base_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_operation_logs_filter ON operation_logs(operation, status, source, created_at DESC);`,
	}

	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize sqlite operation log schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteOperationLogStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteOperationLogStore) Append(entry model.OperationLogEntry) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite operation log store is nil")
	}
	if strings.TrimSpace(entry.ID) == "" {
		return fmt.Errorf("operation log id is required")
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("encode operation log metadata: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin operation log transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(
		`INSERT INTO operation_logs (
			id, correlation_id, operation, source, status, knowledge_base_id, knowledge_base_name,
			document_id, document_name, file_size, size_label, stage, index_status, message,
			error, metadata, started_at, finished_at, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(entry.ID),
		strings.TrimSpace(entry.CorrelationID),
		strings.TrimSpace(entry.Operation),
		strings.TrimSpace(entry.Source),
		strings.TrimSpace(entry.Status),
		strings.TrimSpace(entry.KnowledgeBaseID),
		strings.TrimSpace(entry.KnowledgeBaseName),
		strings.TrimSpace(entry.DocumentID),
		strings.TrimSpace(entry.DocumentName),
		entry.FileSize,
		strings.TrimSpace(entry.SizeLabel),
		strings.TrimSpace(entry.Stage),
		strings.TrimSpace(entry.IndexStatus),
		strings.TrimSpace(entry.Message),
		strings.TrimSpace(entry.Error),
		string(metadataJSON),
		normalizeOperationLogTimestamp(entry.StartedAt),
		normalizeOperationLogTimestamp(entry.FinishedAt),
		entry.DurationMs,
		normalizeOperationLogTimestamp(entry.CreatedAt),
	); err != nil {
		return fmt.Errorf("insert operation log: %w", err)
	}

	if err = s.pruneLocked(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit operation log transaction: %w", err)
	}
	return nil
}

func (s *SQLiteOperationLogStore) pruneLocked(tx *sql.Tx) error {
	retention := s.retention
	if retention <= 0 {
		retention = defaultOperationLogRetention
	}
	_, err := tx.Exec(
		`DELETE FROM operation_logs
		 WHERE id NOT IN (
		   SELECT id FROM operation_logs
		   ORDER BY created_at DESC, id DESC
		   LIMIT ?
		 )`,
		retention,
	)
	if err != nil {
		return fmt.Errorf("prune operation logs: %w", err)
	}
	return nil
}

func (s *SQLiteOperationLogStore) List(query model.OperationLogListQuery) (model.OperationLogListResponse, error) {
	if s == nil || s.db == nil {
		return model.OperationLogListResponse{}, fmt.Errorf("sqlite operation log store is nil")
	}

	limit := query.Limit
	if limit <= 0 {
		limit = defaultOperationLogLimit
	}
	if limit > maxOperationLogLimit {
		limit = maxOperationLogLimit
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	whereClause, args := buildOperationLogWhereClause(query)
	var total int
	countArgs := append([]any(nil), args...)
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM operation_logs`+whereClause, countArgs...).Scan(&total); err != nil {
		return model.OperationLogListResponse{}, fmt.Errorf("count operation logs: %w", err)
	}

	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, limit, offset)
	rows, err := s.db.Query(
		`SELECT id, correlation_id, operation, source, status, knowledge_base_id, knowledge_base_name,
		        document_id, document_name, file_size, size_label, stage, index_status, message,
		        error, metadata, started_at, finished_at, duration_ms, created_at
		   FROM operation_logs`+whereClause+`
		  ORDER BY created_at DESC, id DESC
		  LIMIT ? OFFSET ?`,
		listArgs...,
	)
	if err != nil {
		return model.OperationLogListResponse{}, fmt.Errorf("list operation logs: %w", err)
	}
	defer rows.Close()

	items := make([]model.OperationLogEntry, 0)
	for rows.Next() {
		var item model.OperationLogEntry
		var metadataJSON string
		if err := rows.Scan(
			&item.ID,
			&item.CorrelationID,
			&item.Operation,
			&item.Source,
			&item.Status,
			&item.KnowledgeBaseID,
			&item.KnowledgeBaseName,
			&item.DocumentID,
			&item.DocumentName,
			&item.FileSize,
			&item.SizeLabel,
			&item.Stage,
			&item.IndexStatus,
			&item.Message,
			&item.Error,
			&metadataJSON,
			&item.StartedAt,
			&item.FinishedAt,
			&item.DurationMs,
			&item.CreatedAt,
		); err != nil {
			return model.OperationLogListResponse{}, fmt.Errorf("scan operation log: %w", err)
		}
		item.Metadata = map[string]any{}
		if strings.TrimSpace(metadataJSON) != "" {
			_ = json.Unmarshal([]byte(metadataJSON), &item.Metadata)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return model.OperationLogListResponse{}, fmt.Errorf("iterate operation logs: %w", err)
	}

	return model.OperationLogListResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func buildOperationLogWhereClause(query model.OperationLogListQuery) (string, []any) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 4)
	add := func(column string, value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		conditions = append(conditions, column+" = ?")
		args = append(args, trimmed)
	}

	add("knowledge_base_id", query.KnowledgeBaseID)
	add("operation", query.Operation)
	add("status", query.Status)
	add("source", query.Source)
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func normalizeOperationLogTimestamp(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return trimmed
}
