package analysis

// store.go 封装 Agent 运行数据的持久化，
// 维护 Run、Step、ToolCall、Artifact 的创建、更新和聚合查询。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrNotFound 表示指定分析任务不存在或不属于当前用户。
var ErrNotFound = errors.New("analysis run not found")

// Store 负责分析任务及其步骤、工具调用和产物的持久化。
type Store struct {
	db *gorm.DB
}

// NewStore 创建分析任务存储。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Migrate 创建或更新分析任务相关的四张表。
func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("analysis database is not configured")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(
		&Run{},
		&RunStep{},
		&ToolCall{},
		&Artifact{},
	); err != nil {
		return fmt.Errorf("migrate analysis tables: %w", err)
	}
	return nil
}

// CreateRun 创建分析任务，并自动生成缺失的任务 ID 和计划快照。
func (s *Store) CreateRun(ctx context.Context, run *Run) error {
	if run == nil {
		return errors.New("run is nil")
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if err := encodePlan(run); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create analysis run: %w", err)
	}
	return nil
}

// GetRun 按用户和任务 ID 查询单个分析任务。
func (s *Store) GetRun(ctx context.Context, userID uint64, runID string) (*Run, error) {
	if _, err := uuid.Parse(runID); err != nil {
		return nil, ErrNotFound
	}

	var run Run
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", runID, userID).
		First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get analysis run: %w", err)
	}
	if err := decodePlan(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns 按创建时间倒序返回用户的分析任务；可按会话过滤。
func (s *Store) ListRuns(
	ctx context.Context,
	userID uint64,
	sessionID string,
	limit int,
) ([]Run, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := s.db.WithContext(ctx).
		Where("user_id = ?", userID)
	if sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}

	var runs []Run
	if err := query.Order("created_at DESC").Limit(limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list analysis runs: %w", err)
	}
	for i := range runs {
		if err := decodePlan(&runs[i]); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

// HasActiveRun 判断指定会话是否已有排队中或执行中的任务。
func (s *Store) HasActiveRun(
	ctx context.Context,
	userID uint64,
	sessionID string,
) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where(
			"user_id = ? AND session_id = ? AND status IN ?",
			userID,
			sessionID,
			[]string{
				RunStatusQueued,
				RunStatusRunning,
			},
		).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("check active analysis run: %w", err)
	}
	return count > 0, nil
}

// UpdateRun 保存分析任务的最新状态、计划、回答和 Token 用量。
func (s *Store) UpdateRun(ctx context.Context, run *Run) error {
	if run == nil {
		return errors.New("run is nil")
	}
	if err := encodePlan(run); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Save(run).Error; err != nil {
		return fmt.Errorf("update analysis run: %w", err)
	}
	return nil
}

// CreateStep 创建分析执行步骤，并补齐 ID 和开始时间。
func (s *Store) CreateStep(ctx context.Context, step *RunStep) error {
	if step.ID == "" {
		step.ID = uuid.NewString()
	}
	if step.StartedAt.IsZero() {
		step.StartedAt = time.Now()
	}
	if err := s.db.WithContext(ctx).Create(step).Error; err != nil {
		return fmt.Errorf("create run step: %w", err)
	}
	return nil
}

// CompleteStep 将步骤标记为完成，并写入输出、错误和结束时间。
func (s *Store) CompleteStep(
	ctx context.Context,
	stepID string,
	status string,
	output any,
	stepErr error,
) error {
	outputJSON, err := marshalOptional(output)
	if err != nil {
		return err
	}
	now := time.Now()
	updates := map[string]any{
		"status":      status,
		"output_json": outputJSON,
		"finished_at": &now,
	}
	if stepErr != nil {
		updates["error"] = stepErr.Error()
	}
	if err := s.db.WithContext(ctx).
		Model(&RunStep{}).
		Where("id = ?", stepID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("complete run step: %w", err)
	}
	return nil
}

// CreateToolCall 创建工具调用记录，并补齐 ID 和创建时间。
func (s *Store) CreateToolCall(ctx context.Context, call *ToolCall) error {
	if call.ID == "" {
		call.ID = uuid.NewString()
	}
	if call.CreatedAt.IsZero() {
		call.CreatedAt = time.Now()
	}
	if err := s.db.WithContext(ctx).Create(call).Error; err != nil {
		return fmt.Errorf("create tool call: %w", err)
	}
	return nil
}

// CompleteToolCall 记录工具调用状态、结果、错误和耗时。
func (s *Store) CompleteToolCall(
	ctx context.Context,
	callID string,
	status string,
	result string,
	callErr error,
	durationMS int64,
) error {
	now := time.Now()
	updates := map[string]any{
		"status":      status,
		"result":      result,
		"duration_ms": durationMS,
		"finished_at": &now,
	}
	if callErr != nil {
		updates["error"] = callErr.Error()
	}
	if err := s.db.WithContext(ctx).
		Model(&ToolCall{}).
		Where("id = ?", callID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("complete tool call: %w", err)
	}
	return nil
}

// CreateArtifact 保存分析产物，并补齐 ID 和创建时间。
func (s *Store) CreateArtifact(ctx context.Context, artifact *Artifact) error {
	if artifact.ID == "" {
		artifact.ID = uuid.NewString()
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now()
	}
	if err := s.db.WithContext(ctx).Create(artifact).Error; err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	return nil
}

// GetRunDetail 返回任务本身及按执行顺序排列的步骤、工具调用和产物。
func (s *Store) GetRunDetail(
	ctx context.Context,
	userID uint64,
	runID string,
) (*RunDetail, error) {
	run, err := s.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}

	var steps []RunStep
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("sequence ASC").
		Find(&steps).Error; err != nil {
		return nil, fmt.Errorf("list run steps: %w", err)
	}

	var calls []ToolCall
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("created_at ASC").
		Find(&calls).Error; err != nil {
		return nil, fmt.Errorf("list tool calls: %w", err)
	}

	var artifacts []Artifact
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("created_at ASC").
		Find(&artifacts).Error; err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	return &RunDetail{
		Run:       *run,
		Steps:     steps,
		ToolCalls: calls,
		Artifacts: artifacts,
	}, nil
}

// encodePlan 将内存中的计划编码到 Run.PlanJSON。
func encodePlan(run *Run) error {
	if run.Plan == nil {
		return nil
	}
	data, err := json.Marshal(run.Plan)
	if err != nil {
		return fmt.Errorf("encode run plan: %w", err)
	}
	run.PlanJSON = string(data)
	return nil
}

// decodePlan 将 Run.PlanJSON 解码为结构化计划。
func decodePlan(run *Run) error {
	if run.PlanJSON == "" {
		run.Plan = []PlanStep{}
		return nil
	}
	if err := json.Unmarshal([]byte(run.PlanJSON), &run.Plan); err != nil {
		return fmt.Errorf("decode run plan: %w", err)
	}
	return nil
}

// marshalOptional 将可选值序列化为 JSON；nil 返回空字符串。
func marshalOptional(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal run output: %w", err)
	}
	return string(data), nil
}
