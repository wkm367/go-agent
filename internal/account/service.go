package account

// service.go 实现用户账户服务，负责 users 表迁移、注册、
// 密码校验登录和按用户 ID 查询。
import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/yzletter/go-agentery/internal/auth"
	"gorm.io/gorm"
)

var (
	// ErrUsernameTaken 表示用户名已被其他账号占用。
	ErrUsernameTaken = errors.New("username is already taken")
	// ErrInvalidCredential 表示用户名不存在或密码错误。
	ErrInvalidCredential = errors.New("invalid credentials")
	// ErrUserNotFound 表示指定用户不存在。
	ErrUserNotFound = errors.New("user not found")
)

// User 是用户账号表 users 的持久化模型。
type User struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement"`
	Username     string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	PasswordHash string    `gorm:"type:varchar(100);not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

// TableName 指定 GORM 使用的用户表名。
func (User) TableName() string {
	return "users"
}

// Service 封装用户注册、认证和查询所需的数据库操作。
type Service struct {
	db *gorm.DB
}

// NewService 创建用户账号服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Migrate 创建或更新 users 表结构。
func (s *Service) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(&User{}); err != nil {
		return fmt.Errorf("migrate users table: %w", err)
	}
	return nil
}

// Register 创建新用户，并将明文密码转换为 bcrypt 哈希后存储。
func (s *Service) Register(
	ctx context.Context,
	username string,
	password string,
) (User, error) {
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	user := User{
		Username:     username,
		PasswordHash: passwordHash,
	}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// Authenticate 根据用户名查找用户并校验密码。
func (s *Service) Authenticate(
	ctx context.Context,
	username string,
	password string,
) (User, error) {
	var user User
	err := s.db.WithContext(ctx).
		Where("username = ?", username).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrInvalidCredential
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	if !auth.CheckPassword(password, user.PasswordHash) {
		return User{}, ErrInvalidCredential
	}
	return user, nil
}

// GetByID 根据用户主键查询用户信息。
func (s *Service) GetByID(ctx context.Context, userID uint64) (User, error) {
	var user User
	err := s.db.WithContext(ctx).
		Where("id = ?", userID).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by id: %w", err)
	}
	return user, nil
}
