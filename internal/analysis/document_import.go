package analysis

// document_import.go 实现用户知识文档的读取、清洗、切块和内容哈希。
import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// MaxDocumentUploadBytes 是单个用户文档允许上传的最大字节数。
	MaxDocumentUploadBytes = 20 << 20
	// documentChunkRunes 是单个文档片段的目标字符数。
	documentChunkRunes = 900
	// documentChunkOverlapRunes 是相邻片段之间的重叠字符数。
	documentChunkOverlapRunes = 120
	// maxDocumentChunks 限制单个文档生成的片段数量。
	maxDocumentChunks = 500
	// maxDocumentCharacters 限制清洗后的文档字符数。
	maxDocumentCharacters = 1_000_000
	// maxDOCXXMLBytes 限制 DOCX 主文档解压后的 XML 大小，避免压缩炸弹。
	maxDOCXXMLBytes = MaxDocumentUploadBytes * 4
)

type parsedDocument struct {
	Title        string
	DocumentType string
	Text         string
}

// parseDocument 根据扩展名解析 Markdown、纯文本或 DOCX 文档。
func parseDocument(filename string, reader io.Reader) (*parsedDocument, error) {
	extension := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	raw, err := readLimited(reader, MaxDocumentUploadBytes)
	if err != nil {
		return nil, err
	}

	var (
		text         string
		documentType string
	)
	switch extension {
	case ".md", ".markdown":
		text = string(raw)
		documentType = "markdown"
	case ".txt":
		text = string(raw)
		documentType = "text"
	case ".docx":
		text, err = extractDOCXText(raw)
		if err != nil {
			return nil, err
		}
		documentType = "docx"
	default:
		return nil, errors.New("当前只支持 .md、.markdown、.txt 和 .docx 文件")
	}

	text = cleanDocumentText(text)
	if text == "" {
		return nil, errors.New("文档没有可用文本内容")
	}
	if utf8.RuneCountInString(text) > maxDocumentCharacters {
		return nil, fmt.Errorf("文档内容不能超过 %d 个字符", maxDocumentCharacters)
	}

	title := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	return &parsedDocument{
		Title:        strings.TrimSpace(title),
		DocumentType: documentType,
		Text:         text,
	}, nil
}

// chunkDocument 按 Markdown 标题和段落边界切块，长段落再按字符重叠切分。
func chunkDocument(text string) []string {
	sections := splitDocumentSections(text)
	chunks := make([]string, 0, len(sections))
	for _, section := range sections {
		chunks = append(chunks, chunkDocumentSection(section)...)
	}
	return chunks
}

// documentContentHash 对文档类型和清洗后的正文计算稳定内容指纹。
func documentContentHash(documentType, text string) string {
	sum := sha256.Sum256([]byte(documentType + "\x00" + text))
	return fmt.Sprintf("%x", sum)
}

type documentSection struct {
	Heading string
	Body    string
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("文档内容为空")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取文档失败: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("文档不能超过 %dMB", maxBytes>>20)
	}
	return data, nil
}

// cleanDocumentText 统一换行、移除控制字符，并压缩连续空行。
func cleanDocumentText(value string) string {
	value = strings.TrimPrefix(value, "\ufeff")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ToValidUTF8(value, "")

	var builder strings.Builder
	for _, char := range value {
		switch {
		case char == '\n' || char == '\t':
			builder.WriteRune(char)
		case char < 0x20:
			continue
		default:
			builder.WriteRune(char)
		}
	}

	lines := strings.Split(builder.String(), "\n")
	cleaned := make([]string, 0, len(lines))
	blankLines := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blankLines++
			if blankLines > 2 {
				continue
			}
			cleaned = append(cleaned, "")
			continue
		}
		blankLines = 0
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// extractDOCXText 只读取 DOCX 主文档 XML，并限制解压后的正文大小。
func extractDOCXText(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("解析 DOCX 失败: %w", err)
	}
	for _, file := range reader.File {
		if filepath.ToSlash(file.Name) != "word/document.xml" {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("打开 DOCX 正文失败: %w", err)
		}
		defer stream.Close()

		limited := io.LimitReader(stream, maxDOCXXMLBytes+1)
		content, err := io.ReadAll(limited)
		if err != nil {
			return "", fmt.Errorf("读取 DOCX 正文失败: %w", err)
		}
		if int64(len(content)) > maxDOCXXMLBytes {
			return "", errors.New("DOCX 解压后的正文过大")
		}
		return decodeWordDocument(content)
	}
	return "", errors.New("DOCX 缺少 word/document.xml")
}

// decodeWordDocument 提取 Word 段落文本，忽略样式信息。
func decodeWordDocument(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var builder strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("解析 DOCX XML 失败: %w", err)
		}

		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				inText = true
			case "tab":
				builder.WriteByte('\t')
			case "br":
				builder.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				builder.Write([]byte(typed))
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "t":
				inText = false
			case "p":
				builder.WriteByte('\n')
			}
		}
	}
	return builder.String(), nil
}

// splitDocumentSections 按 Markdown 标题栈切分章节，让子章节继承父级标题。
func splitDocumentSections(text string) []documentSection {
	lines := strings.Split(text, "\n")
	sections := make([]documentSection, 0, 4)
	current := documentSection{}
	body := make([]string, 0, len(lines))
	headings := make([]string, 0, 6)

	flush := func() {
		current.Body = strings.TrimSpace(strings.Join(body, "\n"))
		if current.Heading != "" || current.Body != "" {
			sections = append(sections, current)
		}
		body = body[:0]
	}

	for _, line := range lines {
		level, heading, ok := parseMarkdownHeading(line)
		if ok {
			flush()
			if level <= len(headings) {
				headings = headings[:level-1]
			}
			headings = append(headings, heading)
			current = documentSection{Heading: strings.Join(headings, "\n")}
			continue
		}
		body = append(body, line)
	}
	flush()

	if len(sections) == 0 {
		return []documentSection{{Body: strings.TrimSpace(text)}}
	}
	return sections
}

func parseMarkdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) || (trimmed[level] != ' ' && trimmed[level] != '\t') {
		return 0, "", false
	}
	return level, trimmed, true
}

// chunkDocumentSection 优先按段落合并内容，并为每个片段保留标题路径。
func chunkDocumentSection(section documentSection) []string {
	limit := documentChunkRunes
	prefix := strings.TrimSpace(section.Heading)
	if prefix != "" {
		limit -= utf8.RuneCountInString(prefix) + 2
	}
	if limit < 120 {
		limit = 120
	}

	blocks := paragraphBlocks(section.Body, limit, documentChunkOverlapRunes)
	if len(blocks) == 0 {
		if prefix == "" {
			return nil
		}
		return []string{prefix}
	}

	chunks := make([]string, 0, len(blocks))
	current := ""
	flush := func() {
		current = strings.TrimSpace(current)
		if current == "" {
			return
		}
		if prefix != "" {
			chunks = append(chunks, prefix+"\n\n"+current)
		} else {
			chunks = append(chunks, current)
		}
	}

	for _, block := range blocks {
		if current == "" {
			current = block
			continue
		}
		candidate := current + "\n\n" + block
		if utf8.RuneCountInString(candidate) <= limit {
			current = candidate
			continue
		}
		flush()
		current = tailRunes(current, documentChunkOverlapRunes) + "\n\n" + block
	}
	flush()
	return chunks
}

// paragraphBlocks 避免在普通段落中间切块，超长段落才按字符重叠拆分。
func paragraphBlocks(text string, limit, overlap int) []string {
	paragraphs := strings.Split(text, "\n\n")
	blocks := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if utf8.RuneCountInString(paragraph) <= limit {
			blocks = append(blocks, paragraph)
			continue
		}
		blocks = append(blocks, splitRunesWithOverlap(paragraph, limit, overlap)...)
	}
	return blocks
}

// splitRunesWithOverlap 按 Unicode 字符切分超长段落，减少边界信息丢失。
func splitRunesWithOverlap(text string, limit, overlap int) []string {
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	if overlap < 0 || overlap >= limit {
		overlap = limit / 5
	}
	step := limit - overlap
	result := make([]string, 0, len(runes)/step+1)
	for start := 0; start < len(runes); start += step {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		result = append(result, strings.TrimSpace(string(runes[start:end])))
		if end == len(runes) {
			break
		}
	}
	return result
}

func tailRunes(value string, count int) string {
	if count <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= count {
		return string(runes)
	}
	return string(runes[len(runes)-count:])
}
