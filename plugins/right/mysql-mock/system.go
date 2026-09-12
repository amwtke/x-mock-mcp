package main

import (
	"context"
	"encoding/json"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"strconv"
	"strings"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

var defaultVariables = map[string]string{
	"version": "8.0.0-xmock", "version_comment": "X-Mock-MCP bounded mock", "autocommit": "1",
	"character_set_client": "utf8mb4", "character_set_connection": "utf8mb4", "character_set_results": "utf8mb4", "character_set_server": "utf8mb4", "collation_connection": "utf8mb4_bin", "collation_server": "utf8mb4_bin",
	"time_zone": "+00:00", "system_time_zone": "UTC", "transaction_isolation": "READ-COMMITTED", "tx_isolation": "READ-COMMITTED", "transaction_read_only": "0", "tx_read_only": "0",
	"auto_increment_increment": "1", "lower_case_table_names": "0", "max_allowed_packet": "1048576", "net_buffer_length": "16384", "net_write_timeout": "60", "wait_timeout": "28800", "interactive_timeout": "28800",
	"sql_mode": "STRICT_TRANS_TABLES", "init_connect": "", "performance_schema": "0", "query_cache_size": "0", "query_cache_type": "OFF", "license": "GPL", "language": "english", "local_infile": "0",
}

func (s *state) system(ctx context.Context, id, sql, database string) (json.RawMessage, bool, error) {
	nodes, _, err := parser.New().ParseSQL(sql)
	if err != nil || len(nodes) != 1 {
		return nil, false, nil
	}
	switch n := nodes[0].(type) {
	case *ast.SetStmt:
		for _, v := range n.Variables {
			name := strings.ToLower(v.Name)
			literal, ok := v.Value.(ast.ValueExpr)
			if !ok {
				return nil, true, dbError(1235, "42000", "unsupported SET expression")
			}
			value := ""
			switch x := literal.GetValue().(type) {
			case nil:
				value = "NULL"
			case string:
				value = x
			case int64:
				value = strconv.FormatInt(x, 10)
			case uint64:
				value = strconv.FormatUint(x, 10)
			default:
				return nil, true, dbError(1235, "42000", "unsupported SET value")
			}
			if v.IsGlobal {
				return nil, true, dbError(1235, "42000", "global SET unsupported")
			}
			if name == "autocommit" {
				if _, _, err = s.control(ctx, id, "SET autocommit="+value); err != nil {
					return nil, true, err
				}
				continue
			}
			if name == "transaction_isolation" || name == "tx_isolation" {
				if value != "READ-COMMITTED" {
					return nil, true, dbError(1235, "42000", "unsupported isolation")
				}
			}
			switch name {
			case "names", "character_set_client", "character_set_connection", "character_set_results":
				if value != "utf8mb4" && value != "utf8" && !(name == "character_set_results" && value == "NULL") {
					return nil, true, dbError(1235, "42000", "unsupported character set")
				}
			case "collation_connection":
				if value != "utf8mb4_bin" {
					return nil, true, dbError(1235, "42000", "unsupported collation")
				}
			case "time_zone":
				if value != "+00:00" && value != "UTC" && value != "SYSTEM" {
					return nil, true, dbError(1235, "42000", "unsupported time zone")
				}
			case "transaction_isolation", "tx_isolation":
			case "transaction_read_only", "tx_read_only":
				if value != "0" && value != "1" {
					return nil, true, dbError(1235, "42000", "unsupported read-only value")
				}
			case "sql_select_limit":
				if strings.ToUpper(value) != "DEFAULT" {
					return nil, true, dbError(1235, "42000", "session SELECT limit unsupported")
				}
			default:
				return nil, true, dbError(1235, "42000", "unknown SET variable: "+name)
			}
			s.mu.Lock()
			c := s.conn(id)
			if name == "names" {
				for _, key := range []string{"character_set_client", "character_set_connection", "character_set_results"} {
					c.settings[key] = value
				}
			} else {
				c.settings[name] = value
			}
			if name == "transaction_read_only" || name == "tx_read_only" {
				c.readOnly = value == "1"
			}
			s.mu.Unlock()
		}
		s.mu.Lock()
		raw := okResult(s.conn(id), 0, 0)
		s.mu.Unlock()
		return raw, true, nil
	case *ast.SelectStmt:
		if n.From != nil {
			return nil, false, nil
		}
		if n.Where != nil || n.GroupBy != nil || n.Having != nil || n.Limit != nil || n.Distinct {
			return nil, true, dbError(1235, "42000", "unsupported system SELECT clauses")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c := s.conn(id)
		columns := []mysqlv1.Column{}
		row := []json.RawMessage{}
		for _, field := range n.Fields.Fields {
			name := ""
			value := ""
			typ := "VARCHAR"
			switch e := field.Expr.(type) {
			case *ast.VariableExpr:
				if !e.IsSystem {
					return nil, true, dbError(1235, "42000", "user variables unsupported")
				}
				name = strings.ToLower(e.Name)
				v, ok := defaultVariables[name]
				if !ok {
					return nil, true, dbError(1235, "42000", "unknown system variable: "+name)
				}
				value = v
				if changed, ok := c.settings[name]; ok {
					value = changed
				}
				switch name {
				case "autocommit":
					if c.autocommit {
						value = "1"
					} else {
						value = "0"
					}
					typ = "BIGINT"
				case "transaction_read_only", "tx_read_only":
					if c.readOnly {
						value = "1"
					} else {
						value = "0"
					}
					typ = "BIGINT"
				}
				name = "@@" + name
			case *ast.FuncCallExpr:
				if len(e.Args) > 0 {
					return nil, true, dbError(1235, "42000", "system function arguments unsupported")
				}
				name = strings.ToUpper(e.FnName.O) + "()"
				switch strings.ToLower(e.FnName.O) {
				case "database", "schema":
					value = database
				case "version":
					value = defaultVariables["version"]
				case "connection_id":
					value = strconv.Itoa(len(s.sessions))
					typ = "BIGINT"
				case "last_insert_id":
					value = strconv.FormatUint(c.lastInsert, 10)
					typ = "BIGINT"
				default:
					return nil, true, dbError(1235, "42000", "unsupported system function")
				}
			case ast.ValueExpr:
				v, err := literalValue(e, "BIGINT")
				if err != nil {
					return nil, true, dbError(1235, "42000", "only integer probe literals supported")
				}
				value, _ = v.String()
				name = value
				typ = "BIGINT"
			default:
				return nil, true, dbError(1235, "42000", "unsupported system expression")
			}
			if field.AsName.O != "" {
				name = field.AsName.O
			}
			columns = append(columns, mysqlv1.Column{Name: name, Type: typ})
			row = append(row, mysqlv1.Encode(value))
		}
		result := mysqlv1.Rows{Kind: "rows", Columns: columns, Rows: [][]json.RawMessage{row}, Status: status(c)}
		return mysqlv1.Encode(result), true, nil
	}
	return nil, false, nil
}
