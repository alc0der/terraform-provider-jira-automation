package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL    string
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
	Name          string            `json:"name"`
	RuleScopeARIs []string          `json:"ruleScopeARIs,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Trigger       json.RawMessage   `json:"trigger"`
	Components    []json.RawMessage `json:"components"`
}

// CreateRuleResponse is the response from POST /rule.
type CreateRuleResponse struct {
	UUID string `json:"uuid"`
}

// UpdateRuleRequest is the payload for PUT /rule/{uuid}.
type UpdateRuleRequest struct {
	Name          string            `json:"name"`
	RuleScopeARIs []string          `json:"ruleScopeARIs,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Trigger       json.RawMessage   `json:"trigger"`
	Components    []json.RawMessage `json:"components"`
}

// SetRuleStateRequest is the payload for PUT /rule/{uuid}/state.
type SetRuleStateRequest struct {
	Enabled bool `json:"enabled"`
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

	if update.RuleScopeARIs != nil {
		ruleMap["ruleScopeARIs"] = update.RuleScopeARIs
	} else {
		ruleMap["ruleScopeARIs"] = []string{}
	}
	if update.Labels != nil {
		ruleMap["labels"] = update.Labels
	} else {
		ruleMap["labels"] = []string{}
	}

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
	body, err := json.Marshal(SetRuleStateRequest{Enabled: enabled})
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
