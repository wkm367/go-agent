package analysis

// guard.go 实现只读 SQL 安全守卫，校验语句类型、表访问白名单、
// 禁用关键字和长度限制，并统一附加最大返回行数。
import (
	"errors"
	"fmt"
	"strings"

	"github.com/blastrain/vitess-sqlparser/sqlparser"
)

const (
	// MaxQueryRows 是单次 SQL 查询允许返回给模型的最大数据行数。
	MaxQueryRows = 500
	// MaxQuerySQLBytes 是单条 SQL 文本的长度上限。
	MaxQuerySQLBytes = 32 * 1024
)

// allowedAnalyticsTables 是内置分析库允许查询的物理表白名单。
var allowedAnalyticsTables = map[string]struct{}{
	"customers":           {},
	"products":            {},
	"channels":            {},
	"regions":             {},
	"orders":              {},
	"order_items":         {},
	"refunds":             {},
	"inventory_snapshots": {},
}

// ValidateReadonlySQL 使用内置电商表白名单校验并包装只读 SQL。
func ValidateReadonlySQL(sql string) (string, error) {
	return ValidateReadonlySQLWithTables(sql, allowedAnalyticsTables)
}

// ValidateReadonlySQLWithTables 校验 SQL 只能读取指定白名单表，
// 并返回附加最大行数限制后的查询语句。
func ValidateReadonlySQLWithTables(
	sql string,
	allowedTables map[string]struct{},
) (string, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return "", errors.New("SQL 不能为空")
	}
	if len(sql) > MaxQuerySQLBytes {
		return "", errors.New("SQL 超过长度限制")
	}
	if strings.Contains(sql, ";") {
		return "", errors.New("不允许分号或多语句 SQL")
	}
	if strings.Contains(sql, "--") || strings.Contains(sql, "/*") || strings.Contains(sql, "#") {
		return "", errors.New("SQL 注释不允许使用")
	}

	lower := strings.ToLower(sql)
	for _, keyword := range []string{
		" insert ", " update ", " delete ", " replace ", " alter ", " drop ",
		" truncate ", " create ", " grant ", " revoke ", " call ", " load ",
		" into outfile", " into dumpfile", " for update", " lock in share mode",
	} {
		if strings.Contains(" "+lower+" ", keyword) {
			return "", fmt.Errorf("SQL 包含禁止语句: %s", strings.TrimSpace(keyword))
		}
	}

	statement, err := sqlparser.Parse(sql)
	if err != nil {
		return "", fmt.Errorf("SQL 解析失败: %w", err)
	}
	switch statement.(type) {
	case *sqlparser.Select, *sqlparser.Union, *sqlparser.ParenSelect:
	default:
		return "", fmt.Errorf("只允许 SELECT 查询，实际为 %T", statement)
	}

	tables := make(map[string]struct{})
	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		// Column qualifiers such as o.status are represented as TableName nodes.
		// They are not physical tables and must not be checked against the allowlist.
		if _, ok := node.(*sqlparser.ColName); ok {
			return false, nil
		}

		var tableName sqlparser.TableName
		switch value := node.(type) {
		case *sqlparser.TableName:
			tableName = *value
		case sqlparser.TableName:
			tableName = value
		default:
			return true, nil
		}
		if tableName.IsEmpty() {
			return true, nil
		}
		if !tableName.Qualifier.IsEmpty() {
			return false, errors.New("表名不允许携带数据库限定符")
		}
		name := strings.ToLower(tableName.Name.String())
		if _, allowed := allowedTables[name]; !allowed {
			return false, fmt.Errorf("表 %s 不在只读白名单中", name)
		}
		tables[name] = struct{}{}
		return true, nil
	}, statement)
	if err != nil {
		return "", err
	}
	if len(tables) == 0 {
		return "", errors.New("查询必须访问至少一个业务表")
	}

	guarded := fmt.Sprintf(
		"SELECT * FROM (%s) AS __agent_result LIMIT %d",
		sql,
		MaxQueryRows+1,
	)
	return guarded, nil
}
