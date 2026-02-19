# Jira Automation REST API — Tacit Knowledge

## API Base URL

```
https://api.atlassian.com/automation/public/jira/{cloudId}/rest/v1
```

Cloud ID is resolved from `https://{site}/_edge/tenant_info`.

Auth: Basic auth with `email:api_token`.

## The Envelope Rule

**All write operations (POST, PUT) require the payload wrapped in `{"rule": ...}`.**

```json
// WRONG — returns 400
{"name": "My Rule", "trigger": {...}, "components": [...]}

// CORRECT
{"rule": {"name": "My Rule", "trigger": {...}, "components": [...]}}
```

The GET response also uses this envelope (`{"rule": {...}}`), so a round-trip is: unwrap on read, re-wrap on write.

The error message for a missing envelope is the same generic `400: "The request body could not be parsed"` — it gives no hint about the envelope.

## PUT Requires the Full Rule Object

A PUT that only sends the fields you want to change will fail with 400. The API expects **all** fields from the GET response (minus `uuid`, `created`, `updated`).

The correct pattern is **read-modify-write**:
1. GET the rule (full JSON)
2. Remove `uuid`, `created`, `updated`
3. Merge in your changes (name, trigger, components, scope, labels)
4. Wrap in `{"rule": ...}` and PUT

Fields that must be preserved from GET: `state`, `description`, `canOtherRuleTrigger`, `notifyOnError`, `authorAccountId`, `actor`, `writeAccessType`, `collaborators`.

## Component IDs

Components have API-assigned `id`, `parentId`, and `conditionParentId` fields. Key rules:

- **On update:** strip all component IDs. The API will reassign them. If you keep old IDs but change the component tree structure, you get `400: "Component ids do not match the existing rule or there are duplicate ids"`.
- **The `parentId` / `conditionParentId` fields are redundant** with the `children` / `conditions` nesting. Set them to `null` when writing; the API populates them on read.
- **For Terraform state:** strip these fields during normalization so the config (which doesn't have them) matches the state (which would have them from GET).

## IF Condition Structure

An IF block that gates actions requires a **3-layer nesting**:

```
CONDITION (jira.condition.container.block)     — outer wrapper
  └── CONDITION_BLOCK (jira.condition.if.block)  — the IF branch
        ├── conditions: [CONDITION (jira.issue.condition)]  — the check
        └── children: [ACTION, ACTION, ...]                 — guarded actions
```

For "field is not empty":
```json
{
  "component": "CONDITION",
  "type": "jira.issue.condition",
  "schemaVersion": 3,
  "value": {
    "compareValue": null,
    "comparison": "NOT_EMPTY",
    "selectedField": {"type": "ID", "value": "customfield_XXXXX"},
    "selectedFieldType": "customfield_XXXXX"
  }
}
```

An ELSE branch is another `CONDITION_BLOCK` sibling with `conditions: []`.

## API Token Expiry

The `ATLASSIAN_TOKEN` has a short lifetime. An expired token returns the **same** `400: "The request body could not be parsed"` error on writes (not 401). Always verify auth with a quick GET before debugging write failures.

## OpenAPI Spec Location

The full schema is embedded in the docs page source at:
```
https://developer.atlassian.com/cloud/automation/rest/
```
Extract it from `window.__DATA__.schema.components.schemas`. Key schemas: `RuleWriteRequest`, `RuleUpdateRequest`, `ComponentConfigDTO`, `TriggerConfigDTO`.

## Debugging Checklist

When a write operation returns 400:
1. Verify auth — does GET still work?
2. Check the `{"rule": ...}` envelope is present
3. Check you're sending the full rule object (not just changed fields)
4. Check component IDs are stripped (or consistent with the existing rule)
5. If all else fails, extract the OpenAPI spec and validate your payload against the schema
