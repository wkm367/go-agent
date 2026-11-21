package history

// models.go 定义会话、消息和 API 视图结构，
// 映射 chat_sessions、chat_messages 两张历史记录表。
import (
	"errors"
	"time"
)

const (
	// RoleUser 表示消息由用户发送。
	RoleUser = "user"
	// RoleAssistant 表示消息由 Agent 生成。
	RoleAssistant = "assistant"

	// StatusComplete 表示流式回答已正常完成。
	StatusComplete = "complete"
	// StatusPartial 表示流式回答被中断，但已有部分内容可保存。
	StatusPartial = "partial"
)

var (
	// ErrNotFound 表示会话或消息不存在。
	ErrNotFound = errors.New("conversation not found")
	// ErrBusy 表示同一会话已有生成任务正在执行。
	ErrBusy = errors.New("conversation is already generating")
)

// Session 是 chat_sessions 表中的会话记录。
type Session struct {
	ID       string  `gorm:"type:char(36);primaryKey"`
	UserID   uint64  `gorm:"not null;index:idx_chat_sessions_user_updated,priority:1"`
	ClientID *string `gorm:"type:varchar(64)"`
	Title    string  `gorm:"type:varchar(255);not null"`
	// CreatedAt 和 UpdatedAt 用于会话列表排序及更新时间展示。
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null;index:idx_chat_sessions_user_updated,priority:2,sort:desc"`
	// Messages 是 GORM 关联字段；删除会话时数据库会级联删除消息。
	Messages []Message `gorm:"foreignKey:SessionID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

// TableName 指定 GORM 使用的会话表名。
func (Session) TableName() string {
	return "chat_sessions"
}

// Message 是 chat_messages 表中的单条对话消息。
type Message struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	SessionID string `gorm:"type:char(36);not null;index:idx_chat_messages_session_id_id,priority:1"`
	// Role 取值为 RoleUser 或 RoleAssistant。
	Role string `gorm:"type:varchar(16);not null"`
	// Content 保存消息正文。
	Content string `gorm:"type:mediumtext;not null"`
	// Status 表示回答完整度，取值为 StatusComplete 或 StatusPartial。
	Status string `gorm:"type:varchar(16);not null"`
	// CreatedAt 与 SessionID 组合索引，用于按时间读取会话消息。
	CreatedAt time.Time `gorm:"not null;index:idx_chat_messages_session_id_id,priority:2,sort:asc"`
}

// TableName 指定 GORM 使用的消息表名。
func (Message) TableName() string {
	return "chat_messages"
}

// SessionSummary 是会话列表接口返回的摘要结构。
type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MessageView 是消息接口返回的只读视图。
type MessageView struct {
	ID        uint64    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
