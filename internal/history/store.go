package history

// store.go 实现会话历史的 MySQL 持久化、Redis 缓存、
// 最近上下文加载、所有权校验和会话级并发锁。
import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// sessionCacheTTL 是会话列表缓存的存活时间。
	sessionCacheTTL = 5 * time.Minute
	// historyCacheTTL 是最近对话上下文缓存的存活时间。
	historyCacheTTL = 30 * time.Minute
	// chatLockTTL 是会话生成锁的最长持有时间，用于避免异常退出后永久占锁。
	chatLockTTL = 10 * time.Minute
	// cacheKeyPrefix 统一隔离本项目写入 Redis 的键。
	cacheKeyPrefix = "go-agentery"
)

// Store 负责会话历史的持久化、缓存、上下文加载和并发互斥。
type Store struct {
	// db 是主业务 MySQL 连接。
	db *gorm.DB
	// cache 为可选 Redis 缓存；为 nil 时退化为仅使用 MySQL。
	cache Cache
	// contextLimit 是加载给模型的历史消息数量上限。
	contextLimit int
	// localLocks 在 Redis 不可用时提供进程内会话锁。
	localLocks sync.Map
}

// OpenMySQL 创建 GORM MySQL 连接，并配置连接池和启动探活。
func OpenMySQL(ctx context.Context, dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get mysql connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return db, nil
}

// NewStore 创建会话历史存储；非法上下文上限会回退到 40 条消息。
func NewStore(db *gorm.DB, cache Cache, contextLimit int) *Store {
	if contextLimit < 1 {
		contextLimit = 40
	}
	return &Store{
		db:           db,
		cache:        cache,
		contextLimit: contextLimit,
	}
}

// Migrate 创建或更新会话和消息表结构。
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(&Session{}, &Message{}); err != nil {
		return fmt.Errorf("migrate chat history tables: %w", err)
	}
	return nil
}

// PingMySQL 检查主数据库连接是否可用。
func (s *Store) PingMySQL(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// PingRedis 检查 Redis 缓存是否可用。
func (s *Store) PingRedis(ctx context.Context) error {
	if s.cache == nil {
		return errors.New("redis is not configured")
	}
	return s.cache.Ping(ctx)
}

// Close 关闭底层 MySQL 连接池。
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ListSessions 返回指定用户的最近会话摘要，优先读取 Redis 缓存。
func (s *Store) ListSessions(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]SessionSummary, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	cacheKey := sessionListKey(userID)
	if summaries, ok := s.cachedSessions(ctx, cacheKey); ok {
		if len(summaries) > limit {
			return summaries[:limit], nil
		}
		return summaries, nil
	}

	var sessions []Session
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Limit(100).
		Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	summaries := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		summaries = append(summaries, SessionSummary{
			ID:        session.ID,
			Title:     session.Title,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		})
	}
	s.cacheValue(ctx, cacheKey, summaries, sessionCacheTTL)
	if len(summaries) > limit {
		return summaries[:limit], nil
	}
	return summaries, nil
}

// GetMessages 校验会话所有权后返回会话中的全部消息。
func (s *Store) GetMessages(
	ctx context.Context,
	userID uint64,
	sessionID string,
) ([]MessageView, error) {
	if err := s.ensureOwnership(ctx, userID, sessionID); err != nil {
		return nil, err
	}

	var messages []Message
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id ASC").
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	result := make([]MessageView, 0, len(messages))
	for _, message := range messages {
		result = append(result, messageView(message))
	}
	return result, nil
}

// DeleteSession 在事务中删除会话及其消息，并清理相关缓存。
func (s *Store) DeleteSession(ctx context.Context, userID uint64, sessionID string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session Session
		if err := tx.Where("id = ? AND user_id = ?", sessionID, userID).
			First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		if err := tx.Where("session_id = ?", sessionID).Delete(&Message{}).Error; err != nil {
			return err
		}
		return tx.Delete(&session).Error
	})
	if err != nil {
		return err
	}

	s.invalidateSession(ctx, userID, sessionID)
	return nil
}

// LoadConversation 保存用户消息、更新会话信息，并返回最近的历史上下文。
func (s *Store) LoadConversation(
	ctx context.Context,
	userID uint64,
	sessionID string,
	userMessage string,
) ([]*schema.Message, error) {
	if _, err := uuid.Parse(sessionID); err != nil {
		return nil, ErrNotFound
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session Session
		err := tx.Where("id = ?", sessionID).First(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			session = Session{
				ID:     sessionID,
				UserID: userID,
				Title:  buildSessionTitle(userMessage),
			}
			if err := tx.Create(&session).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if session.UserID != userID {
			return ErrNotFound
		} else {
			updates := map[string]any{"updated_at": time.Now()}
			if strings.TrimSpace(session.Title) == "" {
				updates["title"] = buildSessionTitle(userMessage)
			}
			if err := tx.Model(&session).Updates(updates).Error; err != nil {
				return err
			}
		}

		message := Message{
			SessionID: sessionID,
			Role:      RoleUser,
			Content:   userMessage,
			Status:    StatusComplete,
		}
		return tx.Create(&message).Error
	})
	if err != nil {
		return nil, fmt.Errorf("save user message: %w", err)
	}

	s.invalidateSession(ctx, userID, sessionID)
	return s.recentConversation(ctx, sessionID, s.contextLimit)
}

// SaveAssistantMessage 保存 Agent 回答并更新所属会话的更新时间。
func (s *Store) SaveAssistantMessage(
	ctx context.Context,
	sessionID string,
	content string,
	status string,
) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if status != StatusPartial {
		status = StatusComplete
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		message := Message{
			SessionID: sessionID,
			Role:      RoleAssistant,
			Content:   content,
			Status:    status,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		return tx.Model(&Session{}).
			Where("id = ?", sessionID).
			Update("updated_at", time.Now()).Error
	})
	if err != nil {
		return fmt.Errorf("save assistant message: %w", err)
	}

	var session Session
	if err := s.db.WithContext(ctx).
		Select("user_id").
		Where("id = ?", sessionID).
		First(&session).Error; err == nil {
		s.invalidateSession(ctx, session.UserID, sessionID)
	}
	return nil
}

// AcquireChat 获取会话级生成锁，返回值用于释放锁。
// Redis 不可用时自动退化为进程内互斥锁。
func (s *Store) AcquireChat(
	ctx context.Context,
	sessionID string,
) (func(), error) {
	key := lockKey(sessionID)
	if s.cache != nil {
		token, acquired, err := s.cache.AcquireLock(ctx, key, chatLockTTL)
		if err == nil {
			if !acquired {
				return nil, ErrBusy
			}
			return func() {
				releaseContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := s.cache.ReleaseLock(releaseContext, key, token); err != nil {
					log.Printf("release redis chat lock: %v", err)
				}
			}, nil
		}
		log.Printf("redis chat lock unavailable, using local lock: %v", err)
	}

	value, _ := s.localLocks.LoadOrStore(sessionID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	if !mutex.TryLock() {
		return nil, ErrBusy
	}
	return mutex.Unlock, nil
}

// recentConversation 读取最近 limit 条消息，并按时间正序返回模型消息。
func (s *Store) recentConversation(
	ctx context.Context,
	sessionID string,
	limit int,
) ([]*schema.Message, error) {
	cacheKey := historyKey(sessionID, limit)
	var cached []MessageView
	if ok, err := s.cacheGet(ctx, cacheKey, &cached); err != nil {
		log.Printf("read history cache: %v", err)
	} else if ok {
		return conversationFromViews(cached), nil
	}

	var messages []Message
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id DESC").
		Limit(limit).
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("load conversation context: %w", err)
	}

	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}

	views := make([]MessageView, 0, len(messages))
	for _, message := range messages {
		views = append(views, messageView(message))
	}
	s.cacheValue(ctx, cacheKey, views, historyCacheTTL)
	return conversationFromViews(views), nil
}

// ensureOwnership 确认会话存在且属于指定用户。
func (s *Store) ensureOwnership(ctx context.Context, userID uint64, sessionID string) error {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("check session ownership: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// cachedSessions 尝试从缓存读取会话摘要；缓存不可用时返回未命中。
func (s *Store) cachedSessions(ctx context.Context, key string) ([]SessionSummary, bool) {
	var summaries []SessionSummary
	ok, err := s.cacheGet(ctx, key, &summaries)
	if err != nil {
		log.Printf("read session list cache: %v", err)
		return nil, false
	}
	return summaries, ok
}

// cacheGet 在缓存可用时读取并反序列化指定键。
func (s *Store) cacheGet(ctx context.Context, key string, destination any) (bool, error) {
	if s.cache == nil {
		return false, nil
	}
	return s.cache.Get(ctx, key, destination)
}

// cacheValue 尽力写入缓存；缓存写入失败不会影响主流程。
func (s *Store) cacheValue(ctx context.Context, key string, value any, ttl time.Duration) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Set(ctx, key, value, ttl); err != nil {
		log.Printf("write cache %s: %v", key, err)
	}
}

// invalidateSession 删除会话列表和上下文缓存，避免返回过期数据。
func (s *Store) invalidateSession(ctx context.Context, userID uint64, sessionID string) {
	if s.cache == nil {
		return
	}

	keys := []string{
		historyKey(sessionID, s.contextLimit),
		sessionListKey(userID),
	}
	if err := s.cache.Delete(ctx, keys...); err != nil {
		log.Printf("invalidate session cache: %v", err)
	}
}

// conversationFromViews 将数据库消息视图转换为 Eino 对话消息。
func conversationFromViews(views []MessageView) []*schema.Message {
	messages := make([]*schema.Message, 0, len(views))
	for _, view := range views {
		role := schema.User
		if view.Role == RoleAssistant {
			role = schema.Assistant
		}
		messages = append(messages, &schema.Message{
			Role:    role,
			Content: view.Content,
		})
	}
	return messages
}

// messageView 将持久化消息转换为 API 返回结构。
func messageView(message Message) MessageView {
	return MessageView{
		ID:        message.ID,
		Role:      message.Role,
		Content:   message.Content,
		Status:    message.Status,
		CreatedAt: message.CreatedAt,
	}
}

// buildSessionTitle 将用户首条消息压缩为最多 40 个字符的会话标题。
func buildSessionTitle(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	if title == "" {
		return "新会话"
	}
	if utf8.RuneCountInString(title) <= 40 {
		return title
	}

	runes := []rune(title)
	return string(runes[:40])
}

// sessionListKey 生成用户会话列表的 Redis 键。
func sessionListKey(userID uint64) string {
	return fmt.Sprintf("%s:sessions:%d", cacheKeyPrefix, userID)
}

// historyKey 生成指定会话和上下文长度的 Redis 键。
func historyKey(sessionID string, limit int) string {
	return fmt.Sprintf("%s:history:%s:%d", cacheKeyPrefix, sessionID, limit)
}

// lockKey 生成会话生成锁的 Redis 键。
func lockKey(sessionID string) string {
	return fmt.Sprintf("%s:lock:%s", cacheKeyPrefix, sessionID)
}
