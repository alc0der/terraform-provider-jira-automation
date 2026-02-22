---
page_title: "jira-automation_rule Resource - Jira Automation"
subcategory: ""
description: |-
  Manages a single Jira Automation rule.
---

# jira-automation_rule (Resource)

Manages a single Jira Automation rule.

## Example Usage

```hcl
resource "jira-automation_rule" "example" {
  name    = "My Rule"
  enabled = true

  labels = ["CI"]

  trigger_json = jsonencode({
    component     = "TRIGGER"
    schemaVersion = 1
    type          = "jira.issue.event.trigger:transitioned"
    value = {
      fromStatus = [{ type = "NAME", value = "To Do" }]
      toStatus   = [{ type = "NAME", value = "In Progress" }]
    }
  })

  components_json = jsonencode([
    {
      component     = "ACTION"
      schemaVersion = 1
      type          = "codebarrel.action.log"
      value         = "Hello from Terraform"
    }
  ])
}
```

## Schema

### Required

- `name` (String) - Rule name.
- `trigger_json` (String) - Trigger configuration as JSON. Use `jsonencode()`.
- `components_json` (String) - Actions/conditions array as JSON. Use `jsonencode()`.

### Optional

- `enabled` (Boolean) - Enable or disable the rule. Defaults to `true`.
- `labels` (List of String) - Labels for the rule. The provider auto-adds `managed-by:terraform`.

### Read-Only

- `id` (String) - Rule UUID, set on create or import.
- `state` (String) - `ENABLED` or `DISABLED`.
- `scope` (List of String) - Scope ARIs assigned by the API.

## Import

Import existing rules using an `import` block with the rule UUID:

```hcl
import {
  to = jira-automation_rule.my_rule
  id = "01997721-1866-7233-9bb8-cec4a4614919"
}
```

Then generate the resource configuration:

```bash
terraform plan -generate-config-out=generated.tf
```

~> The Jira Automation API has no DELETE endpoint. Running `terraform destroy` will **disable** the rule instead of deleting it.
