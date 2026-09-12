package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

const mapperNamespace = "local.xmock.ProductMapper"
const mapperSelect = `<select id="product" resultType="map">SELECT id, name, price_cents FROM products WHERE id = #{id}</select>`

func mybatisFixture(t *testing.T, xml string) (pluginapi.PreparationSpec, Input, Bundle) {
	t.Helper()
	spec, in, body := preparationFixture(t)
	in.Sources = append(in.Sources, sourceFile(t, spec.ProjectRoot, "Mapper.xml", "code", xml))
	body.Evidence = in.Sources
	statement := &body.DatabaseScenario.Statements[0]
	statement.SourcePath = "Mapper.xml"
	statement.Source = &StatementSource{Strategy: "mybatis-xml", Namespace: mapperNamespace, StatementID: "product"}
	spec.Input, spec.Candidate = mysqlv1.Encode(in), mysqlv1.Encode(body)
	return spec, in, body
}

func mapperXML(statement string) string {
	return `<mapper namespace="` + mapperNamespace + `">` + statement + `</mapper>`
}

func TestMyBatisStaticXMLSource(t *testing.T) {
	for name, xml := range map[string]string{
		"plain":         mapperXML(mapperSelect),
		"jdbc-type":     mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", "#{id,jdbcType=BIGINT}")),
		"standard-dtd":  `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE mapper PUBLIC "-//mybatis.org//DTD Mapper 3.0//EN" "https://mybatis.org/dtd/mybatis-3-mapper.dtd">` + mapperXML(mapperSelect),
		"result-map":    mapperXML(`<resultMap id="row" type="Product"><id property="id" column="id"/></resultMap>` + strings.ReplaceAll(mapperSelect, `resultType="map"`, `resultMap="row"`)),
		"cdata-comment": mapperXML(`<select id="product" resultType="map"><![CDATA[SELECT id, name, price_cents]]><!-- irrelevant SQL --> FROM products WHERE id = #{id}</select>`),
	} {
		t.Run(name, func(t *testing.T) {
			spec, _, _ := mybatisFixture(t, xml)
			report, err := (&plugin{}).Prepare(context.Background(), spec)
			if err != nil || !report.Ready {
				t.Fatalf("grounded Mapper rejected: %+v %v", report, err)
			}
			var compiled Bundle
			if err = json.Unmarshal(report.CompiledBody, &compiled); err != nil {
				t.Fatal(err)
			}
			if compiled.DatabaseScenario.Statements[0].Source.StatementID != "product" {
				t.Fatal("Mapper evidence lost")
			}
			spec.Candidate = report.CompiledBody
			report, err = (&plugin{}).Prepare(context.Background(), spec)
			if err != nil || !report.Ready {
				t.Fatal("compiled scenario cannot be revalidated", report, err)
			}
		})
	}
}

func TestMyBatisRejectsUnsupportedOrForgedSources(t *testing.T) {
	for name, xml := range map[string]string{
		"missing-condition":   mapperXML(strings.ReplaceAll(mapperSelect, " WHERE id = #{id}", "")),
		"wrong-parameter":     mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", "#{otherId}")),
		"wrong-jdbc-type":     mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", "#{id,jdbcType=VARCHAR}")),
		"unknown-id":          mapperXML(strings.ReplaceAll(mapperSelect, `id="product"`, `id="other"`)),
		"duplicate-id":        mapperXML(mapperSelect + mapperSelect),
		"wrong-namespace":     strings.ReplaceAll(mapperXML(mapperSelect), mapperNamespace, "other.Mapper"),
		"dynamic-if":          mapperXML(strings.ReplaceAll(mapperSelect, "WHERE id = #{id}", `<if test="id != null">WHERE id = #{id}</if>`)),
		"dynamic-foreach":     mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", `<foreach collection="ids" item="id">#{id}</foreach>`)),
		"include":             mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", `<include refid="id"/>`)),
		"select-key":          mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", `<selectKey keyProperty="id">SELECT 1</selectKey>`)),
		"substitution":        mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", "${id}")),
		"type-handler":        mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", "#{id,typeHandler=example.Handler}")),
		"callable":            mapperXML(strings.ReplaceAll(mapperSelect, `id="product"`, `id="product" statementType="CALLABLE"`)),
		"language":            mapperXML(strings.ReplaceAll(mapperSelect, `id="product"`, `id="product" lang="example.Language"`)),
		"database-id":         mapperXML(strings.ReplaceAll(mapperSelect, `id="product"`, `id="product" databaseId="mysql"`)),
		"internal-entity":     `<!DOCTYPE mapper [<!ENTITY query "SELECT id, name, price_cents FROM products WHERE id = #{id}">]>` + mapperXML(`<select id="product">&query;</select>`),
		"external-dtd":        `<!DOCTYPE mapper SYSTEM "https://example.invalid/custom.dtd">` + mapperXML(mapperSelect),
		"extra-root":          mapperXML(mapperSelect) + mapperXML(mapperSelect),
		"comment-only":        mapperXML(`<!-- ` + mapperSelect + ` -->`),
		"sql-tag-mismatch":    mapperXML(strings.ReplaceAll(strings.ReplaceAll(mapperSelect, "<select", "<update"), "</select>", "</update>")),
		"escaped-placeholder": mapperXML(strings.ReplaceAll(mapperSelect, "#{id}", `\#{id}`)),
	} {
		t.Run(name, func(t *testing.T) {
			// Every case hashes its actual mutated XML. Stale SHA detection alone
			// must not hide a missing SQL-source validation rule.
			spec, _, _ := mybatisFixture(t, xml)
			report, err := (&plugin{}).Prepare(context.Background(), spec)
			if err != nil || report.Ready {
				t.Fatalf("bad Mapper accepted: ready=%v err=%v", report.Ready, err)
			}
			if len(report.Diagnostics) == 0 || report.Diagnostics[0].Path != "Mapper.xml" || !strings.Contains(report.Diagnostics[0].Message, "SOURCE_EVIDENCE") || strings.Contains(report.Diagnostics[0].Message, "STALE_EVIDENCE") {
				t.Fatalf("wrong diagnostic: %+v", report)
			}
		})
	}
}

func TestMyBatisRetainsParameterOrderAndLiteralContents(t *testing.T) {
	spec, in, body := mybatisFixture(t, mapperXML(strings.ReplaceAll(mapperSelect, "id = #{id}", "id = #{id} AND price_cents = #{price}")))
	s := &body.DatabaseScenario.Statements[0]
	s.SQL += " AND price_cents = ?"
	s.Parameters = append(s.Parameters, Parameter{Name: "price", Type: "BIGINT", Allowed: []mysqlv1.Value{mysqlv1.Int(9900)}})
	spec.Candidate = mysqlv1.Encode(body)
	report, err := (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatal(report, err)
	}
	s.Parameters[0].Name, s.Parameters[1].Name = s.Parameters[1].Name, s.Parameters[0].Name
	spec.Candidate = mysqlv1.Encode(body)
	report, err = (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || report.Ready {
		t.Fatal("swapped parameters accepted", err)
	}
	// SQL normalization may ignore formatting, but not string literal content.
	xml := mapperXML(strings.ReplaceAll(mapperSelect, "id = #{id}", "id = #{id} AND name = 'two  spaces'"))
	in.Sources[len(in.Sources)-1] = sourceFile(t, spec.ProjectRoot, "Mapper.xml", "code", xml)
	body.Evidence = in.Sources
	s.Parameters = s.Parameters[:1]
	s.Parameters[0].Name = "id"
	s.SQL = "SELECT id, name, price_cents FROM products WHERE id = ? AND name = 'two spaces'"
	spec.Input, spec.Candidate = mysqlv1.Encode(in), mysqlv1.Encode(body)
	report, err = (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || report.Ready {
		t.Fatal("SQL literal changed silently", err)
	}
}
