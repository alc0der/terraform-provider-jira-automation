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

Create a directory for your project (e.g. `my-jira/`) and add a file called `main.tf`:

> **How Terraform reads files:** Terraform loads *all* `.tf` files in the current
> directory and merges them together. File names don't matter — `main.tf`,
> `rules.tf`, `foo.tf` are all treated the same. The convention is to put
> provider config in `main.tf` and resources in other files, but it's just a
> convention.

`main.tf`:

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
| `scope` | list(string) | computed | Scope ARIs assigned by the API |
| `labels` | list(string) | optional | Labels (add-only — the provider auto-adds `managed-by:terraform`) |
| `trigger_json` | string (JSON) | required | Trigger config — use `jsonencode()` |
| `components_json` | string (JSON) | required | Actions/conditions array — use `jsonencode()` |

`trigger_json` and `components_json` use semantic JSON comparison, so whitespace and key ordering differences won't show as drift.

#### Importing an Existing Rule

> **Why not `terraform import`?** The CLI command `terraform import` requires
> you to write the full `resource` block first — including `trigger_json` and
> `components_json`. For automation rules with complex configs, that's
> impractical. The `import` block approach below lets Terraform generate the
> resource configuration from the live API automatically.

To bring a pre-existing Jira Automation rule under Terraform management:

**1. Find the rule UUID.** Open the rule in Jira — the URL contains the ID after `#/rule/`:

```
https://yoursite.atlassian.net/jira/settings/automate#/rule/<rule-uuid>
```

**2. Create `imports.tf`.** In your project directory (next to `main.tf`), create a file called `imports.tf`:

```hcl
import {
  to = jira-automation_rule.my_rule
  id = "01997721-1866-7233-9bb8-cec4a4614919"
}
```

> `my_rule` is the Terraform resource name — you choose it. It's how you'll
> refer to this rule in your `.tf` files. Use something descriptive like
> `attach_test_report` or `notify_on_release`.

**3. Generate the resource configuration.** Terraform can auto-generate the `resource` block from the API. Run this from your project directory:

```bash
terraform plan -generate-config-out=generated.tf
```

This creates a file called `generated.tf` with the full resource block, including `trigger_json` and `components_json` populated from the live rule.

**4. Review and apply.**

```bash
terraform plan    # preview the import
terraform apply   # import into state
```

**5. Clean up.** After the import succeeds, delete `imports.tf` — it was only needed for the one-time import. Rename `generated.tf` to something descriptive (e.g. `rule_my_rule.tf`).

```bash
rm imports.tf
mv generated.tf rule_my_rule.tf
```

**6. Verify.** A subsequent `terraform plan` should show no changes:

```bash
terraform plan
# No changes. Your infrastructure matches the configuration.
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
