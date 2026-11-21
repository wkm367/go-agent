package analysis

// document_import_test.go 验证文档解析、清洗、切块和内容哈希。
import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseMarkdownDocumentCleansAndChunks(t *testing.T) {
	document, err := parseDocument("refund.md", strings.NewReader(
		"\ufeff# 退款规则\r\n\r\n退款率等于退款金额除以支付金额。\r\n\r\n## 例外\r\n\r\n"+
			strings.Repeat("七天无理由退款需要保留订单快照。", 80),
	))
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}
	if document.DocumentType != "markdown" {
		t.Fatalf("expected markdown type, got %s", document.DocumentType)
	}
	if strings.Contains(document.Text, "\r") || strings.HasPrefix(document.Text, "\ufeff") {
		t.Fatalf("expected cleaned text, got %q", document.Text)
	}

	chunks := chunkDocument(document.Text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if !strings.Contains(chunk, "# 退款规则") {
			t.Fatalf("expected heading context in chunk, got %q", chunk)
		}
	}
}

func TestParseDOCXExtractsParagraphText(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create docx entry: %v", err)
	}
	_, _ = file.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>退款规则</w:t></w:r></w:p>
    <w:p><w:r><w:t>退款率等于退款金额除以支付金额。</w:t></w:r></w:p>
  </w:body>
</w:document>`))
	if err := writer.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}

	document, err := parseDocument("refund.docx", bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("parse docx: %v", err)
	}
	if document.DocumentType != "docx" {
		t.Fatalf("expected docx type, got %s", document.DocumentType)
	}
	if !strings.Contains(document.Text, "退款规则") ||
		!strings.Contains(document.Text, "退款率等于退款金额除以支付金额") {
		t.Fatalf("unexpected docx text: %q", document.Text)
	}
}

func TestDocumentContentHashChangesWithText(t *testing.T) {
	first := documentContentHash("text", "退款规则")
	second := documentContentHash("text", "取消规则")
	if first == second {
		t.Fatal("expected different document text to produce different hashes")
	}
}

func TestParseDocumentRejectsUnsupportedFormat(t *testing.T) {
	if _, err := parseDocument("orders.csv", strings.NewReader("a,b")); err == nil {
		t.Fatal("expected CSV to be rejected")
	}
}
