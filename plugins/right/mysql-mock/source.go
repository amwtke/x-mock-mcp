package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// StatementSource belongs to the MySQL right plugin, not the transport or core.
// Omitting it preserves the original Java literal source contract.
type StatementSource struct {
	Strategy    string `json:"strategy"`
	Namespace   string `json:"namespace,omitempty"`
	StatementID string `json:"statement_id,omitempty"`
}

type sourceStrategy interface{ Validate(Statement, []byte) error }

var sourceStrategies = map[string]sourceStrategy{
	"java-literal": javaLiteralSource{},
}

func validateStatementSource(statement Statement, files map[string][]byte) error {
	code, exists := files[statement.SourcePath]
	if !exists {
		return fmt.Errorf("SOURCE_EVIDENCE: source file was not verified")
	}
	name := "java-literal"
	if statement.Source != nil {
		name = statement.Source.Strategy
	}
	strategy, exists := sourceStrategies[name]
	if !exists {
		return fmt.Errorf("SOURCE_EVIDENCE: unsupported strategy %q", name)
	}
	if err := strategy.Validate(statement, code); err != nil {
		return fmt.Errorf("SOURCE_EVIDENCE: %s: %w", statement.ID, err)
	}
	return nil
}

type javaLiteralSource struct{}

func (javaLiteralSource) Validate(statement Statement, code []byte) error {
	if filepath.Ext(statement.SourcePath) != ".java" {
		return fmt.Errorf("java-literal requires .java source; Mapper XML requires mybatis-xml strategy")
	}
	if statement.Source != nil && (statement.Source.Namespace != "" || statement.Source.StatementID != "") {
		return fmt.Errorf("java-literal does not accept Mapper identifiers")
	}
	if strings.TrimSpace(statement.SQL) == "" || !strings.Contains(strings.Join(strings.Fields(string(code)), " "), strings.Join(strings.Fields(statement.SQL), " ")) {
		return fmt.Errorf("源码证据中未找到声明的 SQL；复杂动态 SQL 需要明确分支证据")
	}
	return nil
}
