package api

// dataset_handlers.go 处理文档与 Excel 上传、数据集列表、
// 重命名和删除请求，并把领域错误转换为 HTTP 响应。
import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yzletter/go-agentery/internal/analysis"
)

var allowedDocumentExtensions = map[string]struct{}{
	".md":       {},
	".markdown": {},
	".txt":      {},
	".docx":     {},
}

var allowedExcelExtensions = map[string]struct{}{
	".xlsx": {},
}

// uploadDataset 校验并解析文档，然后为当前用户创建文档数据集。
func (s *apiServer) uploadDataset(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.datasets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据集服务暂不可用"})
		return
	}

	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		analysis.MaxDocumentUploadBytes+(1<<20),
	)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择文档文件"})
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > analysis.MaxDocumentUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文档不能超过 20MB"})
		return
	}
	filename := filepath.Base(fileHeader.Filename)
	extension := strings.ToLower(filepath.Ext(filename))
	if _, allowed := allowedDocumentExtensions[extension]; !allowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前只支持 .md、.markdown、.txt 和 .docx 文件"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件失败"})
		return
	}
	defer file.Close()

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	dataset, err := s.datasets.Create(
		c.Request.Context(),
		userID,
		name,
		filename,
		file,
	)
	if err != nil {
		var duplicate *analysis.DuplicateDatasetError
		if errors.As(err, &duplicate) && duplicate.Existing != nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf(
					"检测到完全相同的文档，已存在于“%s”中",
					duplicate.Existing.Name,
				),
				"duplicate_dataset_id":   duplicate.Existing.ID,
				"duplicate_dataset_name": duplicate.Existing.Name,
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"dataset": dataset})
}

// uploadExcelDataset 解析用户上传的 Excel，创建可被 Agent 查询的私有表。
func (s *apiServer) uploadExcelDataset(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.datasets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据集服务暂不可用"})
		return
	}

	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		analysis.MaxExcelUploadBytes+(1<<20),
	)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择 Excel 文件"})
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > analysis.MaxExcelUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Excel 文件不能超过 20MB"})
		return
	}
	filename := filepath.Base(fileHeader.Filename)
	extension := strings.ToLower(filepath.Ext(filename))
	if _, allowed := allowedExcelExtensions[extension]; !allowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前只支持 .xlsx 文件"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取 Excel 文件失败"})
		return
	}
	defer file.Close()

	name := strings.TrimSpace(c.PostForm("name"))
	sheet := strings.TrimSpace(c.PostForm("sheet"))
	dataset, err := s.datasets.CreateExcel(
		c.Request.Context(),
		userID,
		name,
		filename,
		sheet,
		file,
	)
	if err != nil {
		var duplicate *analysis.DuplicateDatasetError
		if errors.As(err, &duplicate) && duplicate.Existing != nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf(
					"检测到完全相同的 Excel 数据，已存在于“%s”中",
					duplicate.Existing.Name,
				),
				"duplicate_dataset_id":   duplicate.Existing.ID,
				"duplicate_dataset_name": duplicate.Existing.Name,
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"dataset": dataset})
}

// listDatasets 返回当前用户上传的文档和 Excel 数据集。
func (s *apiServer) listDatasets(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.datasets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据集服务暂不可用"})
		return
	}
	datasets, err := s.datasets.List(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "读取数据集失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"datasets": datasets})
}

// renameDataset 修改当前用户拥有的数据集展示名称。
func (s *apiServer) renameDataset(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.datasets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据集服务暂不可用"})
		return
	}
	datasetID := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(datasetID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据集 ID 必须是有效的 UUID"})
		return
	}

	var request struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体必须是有效的 JSON"})
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文档名称不能为空"})
		return
	}

	dataset, err := s.datasets.Rename(
		c.Request.Context(),
		userID,
		datasetID,
		request.Name,
	)
	if err != nil {
		if errors.Is(err, analysis.ErrDatasetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "数据集不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dataset": dataset})
}

// deleteDataset 删除当前用户拥有的数据集及其向量或物理表。
func (s *apiServer) deleteDataset(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.datasets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据集服务暂不可用"})
		return
	}
	datasetID := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(datasetID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据集 ID 必须是有效的 UUID"})
		return
	}
	if err := s.datasets.Delete(c.Request.Context(), userID, datasetID); err != nil {
		if errors.Is(err, analysis.ErrDatasetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "数据集不存在"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "删除数据集失败"})
		return
	}
	c.Status(http.StatusNoContent)
}
