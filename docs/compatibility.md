# v3 CLI contract

The current major version is v3. The executable is `pve`; the Go module is
`github.com/lz-wang/pve-cli/v3`. Configuration lives at
`~/.config/pve/config.yaml`, selected by optional global `--config`/`--profile`.

Documented commands, flag names, positional argument meanings, JSON/YAML fields
and their scalar types remain stable within v3.x. Adding optional flags,
commands and output fields is allowed. Table layout and wording may improve.
Scripts should use JSON/YAML and the exit-code contract.

- Results go to stdout; UPIDs, task progress and diagnostics go to stderr.
- Numeric positional targets mean VMIDs; exact names must resolve uniquely.
- Native `config` objects preserve the remote API's dynamic keys and value types.
- `--yes` skips only local confirmation. Dry-run is read-only. Nonterminal
  dangerous operations require explicit confirmation bypass.
- Operation states distinguish submission, confirmed outcomes, unknown outcomes
  and cancellation requests. A returned UPID is never discarded after waiting.
- Waiting is Context-bound and never implicitly cancels a remote task.
- Non-idempotent requests are not automatically resubmitted.
- Bulk selection must be complete and constrained; results are per target.
- Partial status counts are observed values; `complete=false` and component
  errors must be checked before interpreting them as cluster totals.
- Credentials are file/environment references. Config rejects unknown fields;
  no automatic data migration or compatibility alias layer is implemented.

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | Confirmed operation failure or failed strict health check |
| 2 | Argument or configuration error |
| 3 | API authentication, authorization, connectivity or availability failure |
| 4 | Missing, ambiguous or conflicting resource |
| 5 | Final operation outcome not confirmed |

Removal/renaming of documented commands or fields, field-type changes, changes
to argument meaning, output-stream changes, or weakening required confirmation
need another major version. See [usage](usage.md) and [schema](output-schema.md)
for the complete current contract.
