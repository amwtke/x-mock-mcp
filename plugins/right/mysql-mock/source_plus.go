package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type mybatisPlusSource struct{}

func (mybatisPlusSource) Validate(statement Statement, files map[string][]byte) error {
	s := statement.Source
	if s == nil || s.Plus == nil || s.Plus.Version != "3.5.17" || s.Namespace == "" || s.StatementID == "" || filepath.Ext(statement.SourcePath) != ".java" {
		return fmt.Errorf("explicit MyBatis-Plus 3.5.17 source required")
	}
	for _, path := range []string{s.Plus.EntityPath, s.Plus.MapperPath} {
		if filepath.Ext(path) != ".java" || files[path] == nil {
			return fmt.Errorf("verified entity/Mapper source required: %s", path)
		}
	}
	if err := validatePlusConfiguration(*s, files); err != nil {
		return err
	}
	entity, err := parsePlusEntity(files[s.Plus.EntityPath])
	if err != nil {
		return err
	}
	if err = validatePlusMapper(files[s.Plus.MapperPath], s.Namespace, entity.Name); err != nil {
		return err
	}
	call, err := parsePlusCall(files[statement.SourcePath], *s, entity)
	if err != nil {
		return err
	}
	sql, parameters, err := plusSQL(entity, s.StatementID, call)
	if err != nil {
		return err
	}
	if len(parameters) != len(statement.Parameters) {
		return fmt.Errorf("Plus parameter count mismatch")
	}
	for i, want := range parameters {
		got := statement.Parameters[i]
		if got.Name != want.Name || got.Type != want.Type || got.Nullable {
			return fmt.Errorf("Plus parameter %d must be non-null %s (%s)", i, want.Name, want.Type)
		}
	}
	expected, _, err := canonicalSourceSQL(sql)
	if err != nil {
		return err
	}
	actual, _, err := canonicalSourceSQL(statement.SQL)
	if err != nil {
		return err
	}
	if expected != actual {
		return fmt.Errorf("candidate SQL differs from actual entity/Mapper/Wrapper source; expected %s", sql)
	}
	return nil
}

func plusSQL(entity plusEntity, method string, call plusCall) (string, []Parameter, error) {
	key := entity.key()
	ordered := []plusField{key}
	for _, field := range entity.Fields {
		if !field.Primary {
			ordered = append(ordered, field)
		}
	}
	projection := []string{}
	for _, field := range ordered {
		column := field.Column
		if !strings.EqualFold(strings.ReplaceAll(column, "_", ""), field.Property) {
			column += " AS " + field.Property
		}
		projection = append(projection, column)
	}
	var parameters []Parameter
	param := func(name string, f plusField) string {
		parameters = append(parameters, Parameter{Name: name, Type: f.Type})
		return "?"
	}
	selectSQL := "SELECT " + strings.Join(projection, ",") + " FROM " + entity.Table
	switch method {
	case "selectById":
		return selectSQL + " WHERE " + key.Column + "=" + param("id", key), parameters, nil
	case "deleteById":
		return "DELETE FROM " + entity.Table + " WHERE " + key.Column + "=" + param(key.Property, key), parameters, nil
	case "insert":
		var columns, values []string
		for _, f := range ordered {
			if f.Primary && f.Auto {
				continue
			}
			columns = append(columns, f.Column)
			values = append(values, param(f.Property, f))
		}
		if len(columns) == 0 {
			return "", nil, fmt.Errorf("insert needs non-auto fields")
		}
		return "INSERT INTO " + entity.Table + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(values, ",") + ")", parameters, nil
	case "updateById":
		var sets []string
		for _, f := range ordered {
			if !f.Primary {
				sets = append(sets, f.Column+"="+param("et."+f.Property, f))
			}
		}
		if len(sets) == 0 {
			return "", nil, fmt.Errorf("update needs non-key fields")
		}
		return "UPDATE " + entity.Table + " SET " + strings.Join(sets, ",") + " WHERE " + key.Column + "=" + param("et."+key.Property, key), parameters, nil
	case "selectList", "selectOne", "delete":
		sql := selectSQL
		if method == "delete" {
			sql = "DELETE FROM " + entity.Table
		}
		var conditions, order []string
		for i, f := range call.Predicates {
			conditions = append(conditions, f.Column+"="+param(fmt.Sprintf("ew.paramNameValuePairs.MPGENVAL%d", i+1), f))
		}
		if len(conditions) > 0 {
			sql += " WHERE (" + strings.Join(conditions, " AND ") + ")"
		}
		for _, o := range call.Order {
			column := o.Column
			if o.Descending {
				column += " DESC"
			}
			order = append(order, column)
		}
		if len(order) > 0 {
			sql += " ORDER BY " + strings.Join(order, ",")
		}
		return sql, parameters, nil
	}
	return "", nil, fmt.Errorf("unsupported Plus operation %s", method)
}
