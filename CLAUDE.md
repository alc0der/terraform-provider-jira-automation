# Terraform Provider for Jira Automation

## Building

```bash
go build .
```

The binary is `terraform-provider-jira-automation` (gitignored).

## Running Tests

```bash
go test ./...
```

## Dev Override

Use `dev.tfrc` to point Terraform at the local binary:

```bash
TF_CLI_CONFIG_FILE=dev.tfrc terraform plan
```

No `terraform init` needed with dev overrides.

## import-gen

Regenerates all `rule_*.tf` files in a target directory from the live Jira Automation rules:

```bash
go build -o import-gen ./cmd/import-gen && ./import-gen ../beno
```

Requires `ATLASSIAN_SITE_URL`, `ATLASSIAN_USER`, `ATLASSIAN_TOKEN` env vars.

## API Knowledge

Jira Automation REST API tacit knowledge (envelope requirements, read-modify-write pattern, component IDs, debugging) is in `.claude/skills/jira-automation-api/SKILL.md`.
