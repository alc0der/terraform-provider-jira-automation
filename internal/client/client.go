package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	SiteURL    string
	CloudID    string
	Email      string
	APIToken   string
	HTTPClient *http.Client
}

// TenantInfo is the response from /_edge/tenant_info.
type TenantInfo struct {
	CloudID string `json:"cloudId"`
}

// RuleSummary is a single entry returned by GET /rule/summary.
type RuleSummary struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Enabled bool   `json:"enabled"`
}

// ListRulesResponse is the paginated response from GET /rule/summary.
type ListRulesResponse struct {
	Data   []RuleSummary `json:"data"`
	Cursor *string       `json:"cursor"`
}

// GetRuleResponse is the envelope for GET /rule/{uuid}.
type GetRuleResponse struct {
	Rule json.RawMessage `json:"rule"`
}

// Rule is the full rule object from GET /rule/{uuid}.
type Rule struct {
	UUID          string            `json:"uuid,omitempty"`
	Name          string            `json:"name"`
	State         string            `json:"state,omitempty"`
	RuleScopeARIs []string          `json:"ruleScopeARIs,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Trigger       json.RawMessage   `json:"trigger"`
	Components    []json.RawMessage `json:"components"`
}

// getRuleRaw returns the raw JSON for a rule (without the envelope).
func (c *Client) getRuleRaw(uuid string) (json.RawMessage, error) {
	url := c.BaseURL + "/rule/" + uuid
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building get rule request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting rule %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get rule returned %d: %s", resp.StatusCode, string(body))
	}

	var envelope GetRuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding rule: %w", err)
	}

	return envelope.Rule, nil
}

// CreateRuleRequest is the payload for POST /rule.
type CreateRuleRequest struct {
	Name       string            `json:"name"`
	Trigger    json.RawMessage   `json:"trigger"`
	Components []json.RawMessage `json:"components"`
}

// CreateRuleResponse is the response from POST /rule.
type CreateRuleResponse struct {
	UUID string `json:"uuid"`
}

// UpdateRuleRequest is the payload for PUT /rule/{uuid}.
type UpdateRuleRequest struct {
	Name       string            `json:"name"`
	Trigger    json.RawMessage   `json:"trigger"`
	Components []json.RawMessage `json:"components"`
}

// SetRuleStateRequest is the payload for PUT /rule/{uuid}/state.
type SetRuleStateRequest struct {
	Value string `json:"value"`
}

// New creates a new API client. It resolves the Cloud ID from the site URL.
func New(siteURL, email, apiToken string) (*Client, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Resolve cloud ID from tenant info.
	tenantURL := siteURL + "/_edge/tenant_info"
	req, err := http.NewRequest(http.MethodGet, tenantURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building tenant info request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching tenant info from %s: %w", tenantURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tenant info returned %d: %s", resp.StatusCode, string(body))
	}

	var tenant TenantInfo
	if err := json.NewDecoder(resp.Body).Decode(&tenant); err != nil {
		return nil, fmt.Errorf("decoding tenant info: %w", err)
	}
	if tenant.CloudID == "" {
		return nil, fmt.Errorf("empty cloudId from tenant info")
	}

	baseURL := fmt.Sprintf("https://api.atlassian.com/automation/public/jira/%s/rest/v1", tenant.CloudID)

	return &Client{
		BaseURL:    baseURL,
		SiteURL:    siteURL,
		CloudID:    tenant.CloudID,
		Email:      email,
		APIToken:   apiToken,
		HTTPClient: httpClient,
	}, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.HTTPClient.Do(req)
}

// ListRules returns all rule summaries, handling cursor pagination.
func (c *Client) ListRules() ([]RuleSummary, error) {
	var all []RuleSummary
	url := c.BaseURL + "/rule/summary"

	for {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("building list rules request: %w", err)
		}

		resp, err := c.do(req)
		if err != nil {
			return nil, fmt.Errorf("listing rules: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("list rules returned %d: %s", resp.StatusCode, string(body))
		}

		var page ListRulesResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, fmt.Errorf("decoding list rules response: %w", err)
		}

		all = append(all, page.Data...)

		if page.Cursor == nil || *page.Cursor == "" {
			break
		}
		url = c.BaseURL + "/rule/summary?cursor=" + *page.Cursor
	}

	return all, nil
}

// GetRule returns the full rule config for a given UUID.
func (c *Client) GetRule(uuid string) (*Rule, error) {
	raw, err := c.getRuleRaw(uuid)
	if err != nil {
		return nil, err
	}

	var rule Rule
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, fmt.Errorf("decoding rule: %w", err)
	}

	return &rule, nil
}

// CreateRule creates a new automation rule and returns the UUID.
func (c *Client) CreateRule(rule CreateRuleRequest) (string, error) {
	// The API requires the payload to be wrapped in a {"rule": ...} envelope.
	envelope := struct {
		Rule CreateRuleRequest `json:"rule"`
	}{Rule: rule}
	body, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshaling create rule request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/rule", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building create rule request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("creating rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create rule returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result CreateRuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding create rule response: %w", err)
	}

	return result.UUID, nil
}

// UpdateRule updates an existing automation rule.
// It performs a read-modify-write: fetches the current rule to get all API fields,
// merges in the Terraform-managed fields, strips component IDs (so the API recreates
// them), and PUTs the complete rule wrapped in the required {"rule": ...} envelope.
func (c *Client) UpdateRule(uuid string, update UpdateRuleRequest) error {
	// 1. Fetch current rule as raw JSON to preserve all API-managed fields.
	raw, err := c.getRuleRaw(uuid)
	if err != nil {
		return fmt.Errorf("reading current rule for update: %w", err)
	}

	// 2. Unmarshal into a generic map so we can merge fields.
	var ruleMap map[string]interface{}
	if err := json.Unmarshal(raw, &ruleMap); err != nil {
		return fmt.Errorf("parsing current rule: %w", err)
	}

	// 3. Remove read-only fields that the API won't accept on write.
	delete(ruleMap, "uuid")
	delete(ruleMap, "created")
	delete(ruleMap, "updated")

	// 4. Merge Terraform-managed fields.
	ruleMap["name"] = update.Name

	var trigger interface{}
	if err := json.Unmarshal(update.Trigger, &trigger); err != nil {
		return fmt.Errorf("parsing trigger: %w", err)
	}
	// Strip IDs from trigger.
	stripComponentIDs(trigger)
	ruleMap["trigger"] = trigger

	var components []interface{}
	for _, comp := range update.Components {
		var c interface{}
		if err := json.Unmarshal(comp, &c); err != nil {
			return fmt.Errorf("parsing component: %w", err)
		}
		stripComponentIDs(c)
		components = append(components, c)
	}
	ruleMap["components"] = components

	// 5. Wrap in the required {"rule": ...} envelope.
	envelope := map[string]interface{}{"rule": ruleMap}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshaling update rule request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/rule/"+uuid, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building update rule request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("updating rule %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update rule returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- Internal API for label management ---

// Label is a rule label from the internal API.
type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// internalBaseURL returns the base URL for the internal automation API scoped to a project.
func (c *Client) internalBaseURL(projectID string) string {
	return fmt.Sprintf("%s/gateway/api/automation/internal-api/jira/%s/pro/rest/%s",
		c.SiteURL, c.CloudID, projectID)
}

// ListLabels returns all rule labels for a project via the internal API.
func (c *Client) ListLabels(projectID string) ([]Label, error) {
	url := c.internalBaseURL(projectID) + "/rule-labels"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building list labels request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("listing labels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list labels returned %d: %s", resp.StatusCode, string(body))
	}

	var labels []Label
	if err := json.NewDecoder(resp.Body).Decode(&labels); err != nil {
		return nil, fmt.Errorf("decoding labels: %w", err)
	}

	return labels, nil
}

// CreateLabel creates a new label in a project via the internal API and returns its ID.
func (c *Client) CreateLabel(projectID, name string) (int, error) {
	payload, _ := json.Marshal(map[string]string{"name": name})
	url := c.internalBaseURL(projectID) + "/rule-labels"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("building create label request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return 0, fmt.Errorf("creating label: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("create label returned %d: %s", resp.StatusCode, string(body))
	}

	var label Label
	if err := json.NewDecoder(resp.Body).Decode(&label); err != nil {
		return 0, fmt.Errorf("decoding create label response: %w", err)
	}

	return label.ID, nil
}

// AddLabelToRule associates a label with a rule via the internal API.
func (c *Client) AddLabelToRule(projectID, ruleUUID string, labelID int) error {
	url := fmt.Sprintf("%s/rules/%s/labels/%d", c.internalBaseURL(projectID), ruleUUID, labelID)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("building add label request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("adding label to rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add label returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// EnsureLabel ensures a label with the given name exists in the project and is associated
// with the rule. It looks up the label by name, creates it if missing, then adds it to the rule.
func (c *Client) EnsureLabel(projectID, ruleUUID, labelName string) error {
	labels, err := c.ListLabels(projectID)
	if err != nil {
		return fmt.Errorf("listing labels for ensure: %w", err)
	}

	var labelID int
	for _, l := range labels {
		if l.Name == labelName {
			labelID = l.ID
			break
		}
	}

	if labelID == 0 {
		id, err := c.CreateLabel(projectID, labelName)
		if err != nil {
			return fmt.Errorf("creating label %q: %w", labelName, err)
		}
		labelID = id
	}

	return c.AddLabelToRule(projectID, ruleUUID, labelID)
}

// ExtractProjectID extracts the project ID from a scope ARI string.
// Format: ari:cloud:jira:{cloudId}:project/{projectId}
// Returns empty string if the ARI doesn't match the expected format.
func ExtractProjectID(ari string) string {
	const prefix = ":project/"
	idx := strings.LastIndex(ari, prefix)
	if idx == -1 {
		return ""
	}
	return ari[idx+len(prefix):]
}

// stripComponentIDs recursively removes "id" fields from components so the API
// assigns new IDs. Also nulls out parentId and conditionParentId since the
// parent-child relationships are expressed through nesting.
func stripComponentIDs(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	delete(m, "id")
	m["parentId"] = nil
	m["conditionParentId"] = nil

	if children, ok := m["children"].([]interface{}); ok {
		for _, child := range children {
			stripComponentIDs(child)
		}
	}
	if conditions, ok := m["conditions"].([]interface{}); ok {
		for _, cond := range conditions {
			stripComponentIDs(cond)
		}
	}
}

// SetRuleState enables or disables a rule.
func (c *Client) SetRuleState(uuid string, enabled bool) error {
	stateVal := "DISABLED"
	if enabled {
		stateVal = "ENABLED"
	}
	body, err := json.Marshal(SetRuleStateRequest{Value: stateVal})
	if err != nil {
		return fmt.Errorf("marshaling set rule state request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/rule/"+uuid+"/state", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building set rule state request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("setting rule %s state: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set rule state returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
