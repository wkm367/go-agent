package api

// server_test.go 使用内存假实现覆盖健康检查、认证、
// 会话和数据集路由的关键请求行为。
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/yzletter/go-agentery/internal/analysis"
	"github.com/yzletter/go-agentery/internal/auth"
	"github.com/yzletter/go-agentery/internal/history"
)

const testJWTSecret = "test-secret-for-api-handlers"

type fakeStore struct {
	mysqlErr  error
	redisErr  error
	listErr   error
	deleteErr error
}

type fakeDatasetService struct {
	renamedName string
	excelSheet  string
	excelName   string
}

func (f *fakeDatasetService) Create(
	context.Context,
	uint64,
	string,
	string,
	io.Reader,
) (*analysis.Dataset, error) {
	return nil, nil
}

func (f *fakeDatasetService) CreateExcel(
	_ context.Context,
	_ uint64,
	name string,
	_ string,
	sheet string,
	_ io.Reader,
) (*analysis.Dataset, error) {
	f.excelName = name
	f.excelSheet = sheet
	return &analysis.Dataset{
		ID:          uuid.NewString(),
		Name:        name,
		SourceType:  "excel",
		SheetName:   sheet,
		StorageRows: 3,
		Columns: []analysis.DatasetColumn{
			{Name: "c1", Header: "订单号", Type: "BIGINT"},
			{Name: "c2", Header: "金额", Type: "DOUBLE"},
		},
	}, nil
}

func (f *fakeDatasetService) List(
	context.Context,
	uint64,
) ([]analysis.Dataset, error) {
	return nil, nil
}

func (f *fakeDatasetService) Rename(
	_ context.Context,
	_ uint64,
	datasetID string,
	name string,
) (*analysis.Dataset, error) {
	f.renamedName = name
	return &analysis.Dataset{ID: datasetID, Name: name}, nil
}

func (f *fakeDatasetService) Delete(context.Context, uint64, string) error {
	return nil
}

func (f *fakeStore) PingMySQL(context.Context) error {
	return f.mysqlErr
}

func (f *fakeStore) PingRedis(context.Context) error {
	return f.redisErr
}

func (f *fakeStore) ListSessions(
	context.Context,
	uint64,
	int,
) ([]history.SessionSummary, error) {
	return nil, f.listErr
}

func (f *fakeStore) GetMessages(
	context.Context,
	uint64,
	string,
) ([]history.MessageView, error) {
	return nil, nil
}

func (f *fakeStore) DeleteSession(context.Context, uint64, string) error {
	return f.deleteErr
}

func (f *fakeStore) LoadConversation(
	context.Context,
	uint64,
	string,
	string,
) ([]*schema.Message, error) {
	return nil, nil
}

func TestHealthCheck(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestHealthCheckReturnsServiceUnavailableWhenMySQLIsDown(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(
		&fakeStore{mysqlErr: errors.New("mysql down")},
		nil,
		testJWTSecret,
	).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusServiceUnavailable,
			response.Code,
		)
	}
}

func TestHealthCheckReportsRedisUnavailable(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(
		&fakeStore{redisErr: errors.New("redis down")},
		nil,
		testJWTSecret,
	).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["redis"] != "unavailable" {
		t.Fatalf("expected redis unavailable, got %q", payload["redis"])
	}
}

func TestCORSPreflightAllowsPatch(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodOptions,
		"/api/datasets/"+uuid.NewString(),
		nil,
	)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
	if methods := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, http.MethodPatch) {
		t.Fatalf("expected PATCH in allowed methods, got %q", methods)
	}
}

func TestCreateSessionRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestCreateSession(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	setAuthorization(t, request)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}

	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, err := uuid.Parse(payload.SessionID); err != nil {
		t.Fatalf("expected UUID session_id, got %q", payload.SessionID)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/register",
		strings.NewReader(`{"username":"alice","password":"short"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestDeleteSessionReturnsNotFound(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/sessions/"+uuid.NewString(),
		nil,
	)
	setAuthorization(t, request)
	response := httptest.NewRecorder()

	NewRouter(
		&fakeStore{deleteErr: history.ErrNotFound},
		nil,
		testJWTSecret,
	).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func TestRenameDataset(t *testing.T) {
	datasetID := uuid.NewString()
	datasets := &fakeDatasetService{}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/datasets/"+datasetID,
		strings.NewReader(`{"name":"订单样本"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	setAuthorization(t, request)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret, datasets).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if datasets.renamedName != "订单样本" {
		t.Fatalf("expected renamed dataset, got %q", datasets.renamedName)
	}
}

func TestUploadExcelDataset(t *testing.T) {
	datasets := &fakeDatasetService{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filePart, err := writer.CreateFormFile("file", "orders.xlsx")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := filePart.Write([]byte("xlsx-placeholder")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.WriteField("name", "订单数据"); err != nil {
		t.Fatalf("write name: %v", err)
	}
	if err := writer.WriteField("sheet", "订单"); err != nil {
		t.Fatalf("write sheet: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/datasets/excel",
		&body,
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	setAuthorization(t, request)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret, datasets).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if datasets.excelName != "订单数据" || datasets.excelSheet != "订单" {
		t.Fatalf("unexpected excel import fields: name=%q sheet=%q", datasets.excelName, datasets.excelSheet)
	}

	var payload struct {
		Dataset analysis.Dataset `json:"dataset"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Dataset.SourceType != "excel" || payload.Dataset.StorageRows != 3 {
		t.Fatalf("unexpected dataset: %#v", payload.Dataset)
	}
}

func TestUploadExcelRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/datasets/excel",
		strings.NewReader(""),
	)
	response := httptest.NewRecorder()

	NewRouter(&fakeStore{}, nil, testJWTSecret, &fakeDatasetService{}).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func setAuthorization(t *testing.T, request *http.Request) {
	t.Helper()
	token, err := auth.GenerateToken(testJWTSecret, 1, "alice")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
}
