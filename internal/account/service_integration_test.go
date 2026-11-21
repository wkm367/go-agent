package account_test

// service_integration_test.go 通过 MYSQL_TEST_DSN 验证账号服务
// 在真实 MySQL 上的注册、认证和唯一约束行为。
import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yzletter/go-agentery/internal/account"
	"github.com/yzletter/go-agentery/internal/history"
)

func TestAccountServiceWithMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}

	ctx := context.Background()
	db, err := history.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}

	service := account.NewService(db)
	if err := service.Migrate(ctx); err != nil {
		t.Fatalf("migrate users: %v", err)
	}

	username := "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	t.Cleanup(func() {
		_ = db.Where("username = ?", username).Delete(&account.User{}).Error
	})

	user, err := service.Register(ctx, username, "correct-password")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.ID == 0 || user.Username != username {
		t.Fatalf("unexpected user: %#v", user)
	}

	if _, err := service.Register(ctx, username, "correct-password"); !errors.Is(err, account.ErrUsernameTaken) {
		t.Fatalf("expected duplicate username error, got %v", err)
	}

	authenticated, err := service.Authenticate(ctx, username, "correct-password")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authenticated.ID != user.ID {
		t.Fatalf("expected user id %d, got %d", user.ID, authenticated.ID)
	}

	if _, err := service.Authenticate(ctx, username, "wrong-password"); !errors.Is(err, account.ErrInvalidCredential) {
		t.Fatalf("expected invalid credential error, got %v", err)
	}

	got, err := service.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Username != username {
		t.Fatalf("expected username %q, got %q", username, got.Username)
	}
}
