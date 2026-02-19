package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"terraform-provider-jira-automation/internal/client"
)

func main() {
	outDir := "."
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	siteURL := envFirst("JIRA_SITE_URL", "ATLASSIAN_SITE_URL")
	email := envFirst("JIRA_EMAIL", "ATLASSIAN_USER")
	apiToken := envFirst("JIRA_API_TOKEN", "ATLASSIAN_TOKEN")

	if siteURL == "" || email == "" || apiToken == "" {
		log.Fatal("Set ATLASSIAN_SITE_URL, ATLASSIAN_USER, and ATLASSIAN_TOKEN (or JIRA_* equivalents)")
	}

	c, err := client.New(siteURL, email, apiToken)
	if err != nil {
		log.Fatalf("creating client: %v", err)
	}

	summaries, err := c.ListRules()
	if err != nil {
		log.Fatalf("listing rules: %v", err)
	}

	fmt.Printf("Found %d rules. Fetching full details...\n", len(summaries))

	// Track used resource names to handle duplicates.
	usedNames := map[string]int{}
	var importLines []string

	for i, s := range summaries {
		fmt.Printf("  [%d/%d] %s ... ", i+1, len(summaries), s.Name)

		rule, err := c.GetRule(s.UUID)
		if err != nil {
			fmt.Printf("SKIP (error: %v)\n", err)
			continue
		}

		resName := sanitize(rule.Name)
		if count, exists := usedNames[resName]; exists {
			usedNames[resName] = count + 1
			resName = fmt.Sprintf("%s_%d", resName, count+1)
		} else {
			usedNames[resName] = 1
		}

		hcl := generateHCL(resName, rule)
		filename := fmt.Sprintf("rule_%s.tf", resName)
		path := filepath.Join(outDir, filename)

		if err := os.WriteFile(path, []byte(hcl), 0644); err != nil {
			fmt.Printf("SKIP (write error: %v)\n", err)
			continue
		}

		importLines = append(importLines, fmt.Sprintf(
			"terraform import jira-automation_rule.%s %s", resName, rule.UUID))

		fmt.Printf("-> %s\n", filename)
	}

	// Write imports script.
	importsPath := filepath.Join(outDir, "imports.sh")
	script := "#!/bin/bash\nset -e\n\n" + strings.Join(importLines, "\n") + "\n"
	if err := os.WriteFile(importsPath, []byte(script), 0755); err != nil {
		log.Fatalf("writing imports.sh: %v", err)
	}

	fmt.Printf("\nDone. Generated %d rule files in %s\n", len(importLines), outDir)
	fmt.Printf("Next steps:\n")
	fmt.Printf("  cd %s\n", outDir)
	fmt.Printf("  bash imports.sh\n")
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func sanitize(name string) string {
	s := strings.ToLower(name)
	s = nonAlnum.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "unnamed"
	}
	// Terraform identifiers can't start with a digit.
	if s[0] >= '0' && s[0] <= '9' {
		s = "r_" + s
	}
	return s
}

func generateHCL(resName string, rule *client.Rule) string {
	var b strings.Builder

	enabled := rule.State == "ENABLED"

	fmt.Fprintf(&b, "resource \"jira-automation_rule\" %q {\n", resName)
	fmt.Fprintf(&b, "  name    = %q\n", rule.Name)
	fmt.Fprintf(&b, "  enabled = %v\n", enabled)

	// Scope
	if len(rule.RuleScopeARIs) > 0 {
		fmt.Fprintf(&b, "\n  scope = [\n")
		for _, s := range rule.RuleScopeARIs {
			fmt.Fprintf(&b, "    %q,\n", s)
		}
		fmt.Fprintf(&b, "  ]\n")
	}

	// Labels
	if len(rule.Labels) > 0 {
		fmt.Fprintf(&b, "\n  labels = [\n")
		for _, l := range rule.Labels {
			fmt.Fprintf(&b, "    %q,\n", l)
		}
		fmt.Fprintf(&b, "  ]\n")
	}

	// Trigger JSON — use normalized compact form to match what the provider stores in state.
	triggerNorm := normalizeJSON(rule.Trigger)
	fmt.Fprintf(&b, "\n  trigger_json = %q\n", triggerNorm)

	// Components JSON — use normalized compact form.
	componentsNorm := normalizeJSONArray(rule.Components)
	fmt.Fprintf(&b, "\n  components_json = %q\n", componentsNorm)

	fmt.Fprintf(&b, "}\n")
	return b.String()
}

// normalizeJSON round-trips JSON through interface{} to produce compact, key-sorted output,
// stripping API-assigned fields (id, parentId, conditionParentId).
func normalizeJSON(raw json.RawMessage) string {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	stripAPIFields(v)
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func normalizeJSONArray(raws []json.RawMessage) string {
	var arr []interface{}
	for _, raw := range raws {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			return "[]"
		}
		stripAPIFields(v)
		arr = append(arr, v)
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return "[]"
	}
	return string(out)
}

// stripAPIFields recursively removes API-assigned fields from component JSON.
func stripAPIFields(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	delete(m, "id")
	delete(m, "parentId")
	delete(m, "conditionParentId")

	if children, ok := m["children"].([]interface{}); ok {
		for _, child := range children {
			stripAPIFields(child)
		}
	}
	if conditions, ok := m["conditions"].([]interface{}); ok {
		for _, cond := range conditions {
			stripAPIFields(cond)
		}
	}
}
