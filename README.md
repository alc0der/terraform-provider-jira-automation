# terraform-provider-jira-automation

A Terraform provider for managing Jira Automation rules via the Automation Rule Management REST API.

## Prerequisites

- [Go](https://go.dev/dl/) 1.23+
- [Terraform](https://developer.hashicorp.com/terraform/install) 1.0+
- A Jira Cloud site with an [API token](https://id.atlassian.com/manage-profile/security/api-tokens)

## Setup (step by step)

### 1. Build the provider

```bash
cd terraform-provider-jira-automation
go build -o terraform-provider-jira-automation .
```

This produces a `terraform-provider-jira-automation` binary in the current
directory. **You must rebuild after every code change.** If Terraform says it
can't find the provider executable, this is the step you missed.

### 2. Tell Terraform to use your local build

Terraform normally downloads providers from a registry. During development, we
override that with a `dev.tfrc` file (already included in this directory).
It tells Terraform: "look for the provider binary in this directory instead of
downloading it."

Run this **from the `terraform-provider-jira-automation/` directory**:

```bash
export TF_CLI_CONFIG_FILE="$(pwd)/dev.tfrc"
```

This must be set in the same terminal session where you run `terraform`.
The value persists until you close the terminal.

> You can add this line to your `~/.zshrc` to make it permanent.

### 3. Set environment variables

The provider needs three values. You can pass them via environment variables
so they don't need to be hardcoded in `.tf` files.

Create a `.env` file in the directory where your `.tf` files live (e.g. `beno/.env`):

```bash
ATLASSIAN_SITE_URL=https://yoursite.atlassian.net
ATLASSIAN_USER=you@example.com
ATLASSIAN_TOKEN=your-api-token
```

Then **before running any `terraform` command**, load them into your shell:

```bash
source .env
export ATLASSIAN_SITE_URL ATLASSIAN_USER ATLASSIAN_TOKEN
```

> `source .env` reads the file but only sets shell variables. The `export` line
> is what makes them visible to programs like `terraform`. You need both.

The full list of supported env vars:

| What | Env vars (checked in order) |
|------|-----------------------------|
| Site URL | `JIRA_SITE_URL`, `ATLASSIAN_SITE_URL` |
| Email | `JIRA_EMAIL`, `ATLASSIAN_USER` |
| API Token | `JIRA_API_TOKEN`, `ATLASSIAN_TOKEN` |

### 4. Write your Terraform config

Create a `.tf` file. Two things are required:

1. A `required_providers` block — tells Terraform which provider to use.
2. A `provider` block — **always required by Terraform**, even if empty.

Minimal example using only env vars for credentials:

```hcl
terraform {
  required_providers {
    jira-automation = {
      source = "registry.terraform.io/beno/jira-automation"
    }
  }
}

# This block is always required. When all three values come from env vars,
# it can be empty like this:
provider "jira-automation" {}

data "jira-automation_rules" "all" {}

output "all_rules" {
  value = data.jira-automation_rules.all.rules
}
```

You can also hardcode values directly in the provider block (useful if you
don't want to deal with env vars):

```hcl
provider "jira-automation" {
  site_url  = "https://yoursite.atlassian.net"
  email     = "you@example.com"
  api_token = "your-api-token"   # sensitive — prefer env vars
}
```

Or mix and match — any attribute set in the block takes priority over env vars.

### 5. Run Terraform

```bash
cd beno                  # wherever your .tf files are
source .env && export ATLASSIAN_SITE_URL ATLASSIAN_USER ATLASSIAN_TOKEN
terraform plan           # preview what Terraform will do
terraform apply          # execute it
```

## Provider Configuration Reference

| Attribute   | Type   | In HCL | Env Var Fallback |
|-------------|--------|--------|------------------|
| `site_url`  | string | optional | `JIRA_SITE_URL`, `ATLASSIAN_SITE_URL` |
| `email`     | string | optional | `JIRA_EMAIL`, `ATLASSIAN_USER` |
| `api_token` | string | optional | `JIRA_API_TOKEN`, `ATLASSIAN_TOKEN` |

All three must be provided — either in the provider block, via env vars, or a combination.
The `provider "jira-automation" {}` block itself is always required by Terraform, even if empty.

## Resources

### `jira-automation_rule`

Manages a single Jira Automation rule.

```hcl
resource "jira-automation_rule" "example" {
  name    = "My Rule"
  enabled = true

  scope  = ["ari:cloud:jira:<cloudId>:project/<projectId>"]
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

#### Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | computed | Rule UUID (set on create/import) |
| `name` | string | required | Rule name |
| `enabled` | bool | optional | Enable/disable (default: `true`) |
| `state` | string | computed | `ENABLED` or `DISABLED` |
| `scope` | list(string) | optional | Scope ARIs |
| `labels` | list(string) | optional | Labels |
| `trigger_json` | string (JSON) | required | Trigger config — use `jsonencode()` |
| `components_json` | string (JSON) | required | Actions/conditions array — use `jsonencode()` |

`trigger_json` and `components_json` use semantic JSON comparison, so whitespace and key ordering differences won't show as drift.

#### Import

Import an existing rule by its UUID:

```bash
terraform import jira-automation_rule.example <rule-uuid>
```

#### Destroy behavior

The Jira Automation API has **no DELETE endpoint**. Running `terraform destroy` will **disable** the rule instead of deleting it. A warning is shown when this happens.

## Data Sources

### `jira-automation_rules`

Lists all automation rule summaries.

```hcl
data "jira-automation_rules" "all" {}

output "rules" {
  value = data.jira-automation_rules.all.rules
}
```

Each entry in `rules` has: `uuid`, `name`, `state`, `enabled`.

## Development

```bash
# Build
make build

# Install to ~/.terraform.d/plugins (for use without dev override)
make install

# Run acceptance tests
make testacc
```

## Troubleshooting

**"could not find executable file starting with terraform-provider-jira-automation"** — The binary hasn't been built. Run `go build -o terraform-provider-jira-automation .` from the `terraform-provider-jira-automation/` directory (step 1).

**"requires explicit configuration"** — You need a `provider "jira-automation" {}` block in your `.tf` file. Terraform always requires this, even when all values come from env vars.

**"Missing site_url"** — The env var isn't reaching Terraform. Make sure you ran both `source .env` *and* `export ATLASSIAN_SITE_URL` (or `export JIRA_SITE_URL`). Just `source .env` alone is not enough — you must also `export` the variables.

**"empty cloudId from tenant info"** — Make sure `site_url` is your full Jira Cloud URL (e.g. `https://yoursite.atlassian.net`), not an API URL.

**401 Unauthorized** — Check that your email and API token are correct. API tokens are created at https://id.atlassian.com/manage-profile/security/api-tokens.

**No changes detected after modifying JSON** — The JSON fields use normalized comparison. If only whitespace or key order changed, Terraform correctly sees no diff.
