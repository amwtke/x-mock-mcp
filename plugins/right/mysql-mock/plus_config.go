package main

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"
)

func validatePlusConfiguration(source StatementSource, files map[string][]byte) error {
	p := source.Plus
	raw := files[p.BuildPath]
	if len(raw) == 0 || filepath.Ext(p.BuildPath) != ".xml" {
		return fmt.Errorf("verified Maven POM required")
	}
	var pom struct {
		XMLName      xml.Name `xml:"project"`
		Dependencies []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
			Scope      string `xml:"scope"`
		} `xml:"dependencies>dependency"`
		Profiles  *struct{} `xml:"profiles"`
		Overrides *struct{} `xml:"dependencyManagement"`
	}
	if err := xml.Unmarshal(raw, &pom); err != nil {
		return fmt.Errorf("invalid Plus POM: %w", err)
	}
	if pom.Profiles != nil || pom.Overrides != nil {
		return fmt.Errorf("POM profile/dependency overrides require a separate Plus strategy")
	}
	found := 0
	for _, dep := range pom.Dependencies {
		if dep.GroupID == "com.baomidou" && dep.ArtifactID == "mybatis-plus-spring-boot3-starter" {
			if dep.Version != p.Version || (dep.Scope != "" && dep.Scope != "compile") {
				return fmt.Errorf("Plus starter/version must be explicit %s", p.Version)
			}
			found++
		} else if dep.GroupID == "org.mybatis" || dep.GroupID == "org.mybatis.spring.boot" || dep.GroupID == "com.baomidou" {
			return fmt.Errorf("additional framework dependencies unsupported")
		}
	}
	if found != 1 {
		return fmt.Errorf("one explicit Plus Boot3 starter dependency required")
	}
	raw = files[p.ConfigPath]
	if len(raw) == 0 || filepath.Ext(p.ConfigPath) != ".properties" {
		return fmt.Errorf("verified Plus properties configuration required")
	}
	required := map[string]string{"mybatis-plus.configuration.map-underscore-to-camel-case": "true", "mybatis-plus.configuration.cache-enabled": "false", "mybatis-plus.configuration.local-cache-scope": "STATEMENT"}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if strings.Contains(line, `\`) {
			return fmt.Errorf("escaped Plus properties unsupported")
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || seen[key] {
			return fmt.Errorf("literal unique Plus properties required")
		}
		seen[key] = true
		if expected, exists := required[key]; exists {
			if value != expected {
				return fmt.Errorf("unsupported Plus configuration %s", key)
			}
		} else if strings.HasPrefix(key, "mybatis") {
			return fmt.Errorf("unsupported Plus configuration %s", key)
		}
	}
	for key := range required {
		if !seen[key] {
			return fmt.Errorf("explicit Plus configuration required: %s", key)
		}
	}
	for path, raw := range files {
		if filepath.Ext(path) != ".xml" {
			continue
		}
		var mapper struct {
			XMLName   xml.Name `xml:"mapper"`
			Namespace string   `xml:"namespace,attr"`
		}
		if xml.Unmarshal(raw, &mapper) == nil && mapper.Namespace == source.Namespace {
			return fmt.Errorf("XML override of automatic BaseMapper SQL unsupported")
		}
	}
	return nil
}
