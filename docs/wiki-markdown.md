# Wiki Markdown import

`plane-cli wiki create` and `plane-cli wiki update` can send Markdown source directly to Plane without rendering it in the CLI.

```bash
plane-cli wiki create --name "Runbook" --file runbook.md
plane-cli wiki update <page-id> --file runbook.md
plane-cli wiki create --name "Quick note" --markdown '# Heading'
```

`--file`, `--markdown`, and `--html` are mutually exclusive. `--html` remains available as an explicit compatibility escape hatch.
