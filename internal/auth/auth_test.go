package auth

// auth_test.go 覆盖密码哈希校验、JWT 生成解析，
// 以及过期令牌和非法签名方法的拒绝逻辑。
import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordHashAndCheck(t *testing.T) {
	hash, err := HashPassword("correct-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !CheckPassword("correct-password", hash) {
		t.Fatal("expected password to match")
	}
	if CheckPassword("wrong-password", hash) {
		t.Fatal("expected wrong password not to match")
	}
}

func TestGenerateAndParseToken(t *testing.T) {
	token, err := GenerateToken("test-secret", 42, "alice")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	principal, err := ParseToken("test-secret", token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if principal.UserID != 42 || principal.Username != "alice" {
		t.Fatalf("unexpected principal: %#v", principal)
	}

	if _, err := ParseToken("wrong-secret", token); err == nil {
		t.Fatal("expected signature validation error")
	}
}

func TestParseTokenRejectsExpiredToken(t *testing.T) {
	now := time.Now().Add(-2 * TokenTTL)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Username: "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := ParseToken("test-secret", signed); err == nil {
		t.Fatal("expected expired token error")
	}
}
