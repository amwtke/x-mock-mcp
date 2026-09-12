package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"
)

type mybatisXMLSource struct{}

var mapperProperty = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)

type mapperNode struct {
	name     string
	attrs    map[string]string
	text     strings.Builder
	children []*mapperNode
}

// The standard DTD is recognized locally. encoding/xml never loads it or
// resolves external entities. Reject custom declarations/internal subsets.
func readMapper(raw []byte) (*mapperNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var root *mapperNode
	var stack []*mapperNode
	nodes, directives := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Mapper XML: %w", err)
		}
		switch token := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 4096 || len(stack) >= 64 {
				return nil, fmt.Errorf("Mapper XML exceeds node/depth bound")
			}
			if token.Name.Space != "" {
				return nil, fmt.Errorf("XML namespaces unsupported")
			}
			node := &mapperNode{name: token.Name.Local, attrs: map[string]string{}}
			for _, attr := range token.Attr {
				if attr.Name.Space != "" {
					return nil, fmt.Errorf("XML namespaced attributes unsupported")
				}
				if _, exists := node.attrs[attr.Name.Local]; exists {
					return nil, fmt.Errorf("duplicate XML attribute %s", attr.Name.Local)
				}
				if strings.Contains(attr.Value, "${") {
					return nil, fmt.Errorf("Mapper attribute substitution unsupported")
				}
				node.attrs[attr.Name.Local] = attr.Value
			}
			if len(stack) == 0 {
				if root != nil || node.name != "mapper" {
					return nil, fmt.Errorf("exactly one mapper root required")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(token)) != "" {
					return nil, fmt.Errorf("text outside mapper root")
				}
			} else {
				// MyBatis joins static text/CDATA nodes with spaces, including around XML comments.
				node := stack[len(stack)-1]
				node.text.Write(token)
				node.text.WriteByte(' ')
			}
		case xml.Directive:
			directives++
			value := strings.Join(strings.Fields(strings.ReplaceAll(string(token), "'", `"`)), " ")
			base := `DOCTYPE mapper PUBLIC "-//mybatis.org//DTD Mapper 3.0//EN" `
			if root != nil || directives != 1 || (value != base+`"https://mybatis.org/dtd/mybatis-3-mapper.dtd"` && value != base+`"http://mybatis.org/dtd/mybatis-3-mapper.dtd"`) {
				return nil, fmt.Errorf("only standard MyBatis DTD declaration supported; external entities/subsets forbidden")
			}
		case xml.ProcInst:
			if token.Target != "xml" || root != nil {
				return nil, fmt.Errorf("XML processing instruction unsupported")
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("missing mapper root")
	}
	return root, nil
}

func (mybatisXMLSource) Validate(statement Statement, raw []byte) error {
	source := statement.Source
	if filepath.Ext(statement.SourcePath) != ".xml" || source == nil || !mapperProperty.MatchString(source.Namespace) || !mapperProperty.MatchString(source.StatementID) || strings.Contains(source.StatementID, ".") {
		return fmt.Errorf("mybatis-xml requires .xml source, namespace and local statement_id")
	}
	root, err := readMapper(raw)
	if err != nil {
		return err
	}
	if len(root.attrs) != 1 || root.attrs["namespace"] != source.Namespace || strings.TrimSpace(root.text.String()) != "" {
		return fmt.Errorf("Mapper namespace/root mismatch")
	}
	var selected *mapperNode
	ids := map[string]bool{}
	for _, node := range root.children {
		switch node.name {
		case "resultMap", "sql":
			continue
		case "select", "insert", "update", "delete":
			id := node.attrs["id"]
			if id == "" || ids[id] {
				return fmt.Errorf("empty/duplicate Mapper statement id %q", id)
			}
			ids[id] = true
			if id == source.StatementID {
				selected = node
			}
		default:
			return fmt.Errorf("unsupported Mapper element <%s>", node.name)
		}
	}
	if selected == nil {
		return fmt.Errorf("Mapper statement %s.%s not found", source.Namespace, source.StatementID)
	}
	if len(selected.children) > 0 {
		return fmt.Errorf("%s.%s: unsupported dynamic/nested element <%s>", source.Namespace, source.StatementID, selected.children[0].name)
	}
	allowedAttrs := map[string]bool{"id": true, "parameterType": true, "resultType": true, "resultMap": true, "flushCache": true, "useCache": true, "timeout": true, "fetchSize": true, "statementType": true, "resultSetType": true, "useGeneratedKeys": true, "keyProperty": true, "keyColumn": true}
	for name, value := range selected.attrs {
		if !allowedAttrs[name] {
			return fmt.Errorf("unsupported Mapper statement attribute %s", name)
		}
		if name == "statementType" && value != "PREPARED" {
			return fmt.Errorf("only PREPARED Mapper statements supported")
		}
		if name == "resultSetType" && value != "FORWARD_ONLY" && value != "DEFAULT" {
			return fmt.Errorf("only forward result sets supported")
		}
	}
	rendered, err := mapperParameters(selected.text.String(), statement.Parameters)
	if err != nil {
		return err
	}
	actual, kind, err := canonicalSourceSQL(rendered)
	if err != nil {
		return err
	}
	if kind != selected.name {
		return fmt.Errorf("Mapper element %s disagrees with SQL %s", selected.name, kind)
	}
	expected, _, err := canonicalSourceSQL(statement.SQL)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("Mapper %s.%s SQL differs from candidate", source.Namespace, source.StatementID)
	}
	return nil
}

func mapperParameters(sql string, parameters []Parameter) (string, error) {
	if strings.Contains(sql, "${") || strings.Contains(sql, `\#{`) {
		return "", fmt.Errorf("text substitution/escaped placeholders unsupported")
	}
	var result strings.Builder
	index := 0
	for {
		start := strings.Index(sql, "#{")
		if start < 0 {
			result.WriteString(sql)
			break
		}
		result.WriteString(sql[:start])
		sql = sql[start+2:]
		end := strings.IndexByte(sql, '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated Mapper parameter")
		}
		pieces := strings.Split(sql[:end], ",")
		property := strings.TrimSpace(pieces[0])
		if !mapperProperty.MatchString(property) || index >= len(parameters) || property != parameters[index].Name {
			return "", fmt.Errorf("Mapper parameter %d property/order differs from candidate: %q", index, property)
		}
		seen := map[string]bool{}
		for _, piece := range pieces[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(piece), "=")
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if !ok || key != "jdbcType" || seen[key] || (value != "BIGINT" && value != "VARCHAR") || value != parameters[index].Type {
				return "", fmt.Errorf("unsupported/mismatched Mapper parameter option %q", piece)
			}
			seen[key] = true
		}
		result.WriteByte('?')
		index++
		sql = sql[end+1:]
	}
	if index != len(parameters) {
		return "", fmt.Errorf("Mapper parameter count differs from candidate")
	}
	return result.String(), nil
}

func canonicalSourceSQL(sql string) (string, string, error) {
	nodes, warnings, err := parser.New().ParseSQL(sql)
	if err != nil {
		return "", "", err
	}
	if len(nodes) != 1 || len(warnings) != 0 {
		return "", "", fmt.Errorf("one complete SQL statement required")
	}
	kind := ""
	switch nodes[0].(type) {
	case *ast.SelectStmt:
		kind = "select"
	case *ast.InsertStmt:
		kind = "insert"
	case *ast.UpdateStmt:
		kind = "update"
	default:
		return "", "", fmt.Errorf("only static SELECT/INSERT/UPDATE Mapper statements supported")
	}
	var out strings.Builder
	if err = nodes[0].Restore(format.NewRestoreCtx(format.DefaultRestoreFlags, &out)); err != nil {
		return "", "", err
	}
	return out.String(), kind, nil
}
