package auth

// auth.go 提供 bcrypt 密码哈希与校验、JWT 签发与解析，
// 作为账号服务和 API 鉴权共用的认证原语。
import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	// PasswordCost 是 bcrypt 的计算成本，数值越高越安全但耗时越长。
	PasswordCost = 12
	// TokenTTL 是登录令牌的有效期。
	TokenTTL = 72 * time.Hour
)

// Principal 是完成 JWT 校验后提取出的最小用户身份信息。
type Principal struct {
	UserID   uint64
	Username string
}

// claims 是 JWT 的自定义载荷，包含用户名及标准注册声明。
type claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// HashPassword 使用 bcrypt 对明文密码进行单向哈希。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	return string(hash), err
}

// CheckPassword 校验明文密码是否与 bcrypt 哈希匹配。
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken 为指定用户签发带过期时间的 HS256 JWT。
func GenerateToken(secret string, userID uint64, username string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	})
	return token.SignedString([]byte(secret))
}

// ParseToken 校验 JWT 的签名、算法和有效期，并返回用户身份。
func ParseToken(secret, tokenString string) (Principal, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&claims{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Principal{}, err
	}

	tokenClaims, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return Principal{}, errors.New("invalid token claims")
	}
	if tokenClaims.Username == "" || tokenClaims.Subject == "" {
		return Principal{}, errors.New("token is missing required claims")
	}

	userID, err := strconv.ParseUint(tokenClaims.Subject, 10, 64)
	if err != nil || userID == 0 {
		return Principal{}, errors.New("invalid user id claim")
	}

	return Principal{
		UserID:   userID,
		Username: tokenClaims.Username,
	}, nil
}
