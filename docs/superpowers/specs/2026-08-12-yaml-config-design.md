# YAML config file — design

## Problem

A project's non-derivable settings (title, version, adapter, servers, security,
basePath, accessKey, production) are read from a `specter.json` file, auto-loaded
from the scanned directory or named with `-config`. The loader is JSON-only.
Teams that keep their other tooling config in YAML must maintain a JSON island
just for Specter.

## Goal

Accept the same configuration in YAML (`specter.yaml` / `specter.yml`) alongside
JSON, chosen by file extension. Fully backward compatible: JSON behaviour is
unchanged, and a project with no config still needs none.

## Scope (approved)

- YAML format only. The manual-route-supplement idea (declaring routes the
  scanner cannot resolve) is a separate, later feature — out of scope here.
- Same fields as today, same keys in both formats.

## Design

### Auto-lookup order

With no `-config`, `applyConfigFile` looks in the scanned directory for, in
order: `specter.json`, `specter.yaml`, `specter.yml` — the first that exists is
used. JSON is tried first so today's behaviour is byte-for-byte unchanged; YAML
is a fallback, never a silent override of an existing `specter.json`. A missing
config is still not an error.

### Explicit `-config <path>`

The format is chosen by extension: `.yaml` or `.yml` → YAML, anything else →
JSON (so `.json` and extensionless paths keep decoding as JSON, unchanged). An
explicit path that does not exist is still an error.

### Decoding

`fileConfig` gains `yaml:` struct tags equal to its JSON keys, so a key is spelt
the same in both formats (`title`, `version`, `adapter`, `servers`, `security`,
`basePath`, `accessKey`, `production`). `applyConfigFile` selects the decoder by
the resolved path's extension:

```go
func decodeConfig(path string, data []byte, fc *fileConfig) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return yaml.Unmarshal(data, fc)
	default:
		return json.Unmarshal(data, fc)
	}
}
```

`gopkg.in/yaml.v3` is already a module dependency (used elsewhere in the CLI), so
no new dependency is added. The rest of `applyConfigFile` — the
flag-wins-over-file precedence, the field copies — is unchanged: only the
unmarshal call is routed by extension.

### Servers / security in YAML

`specter.Server` and `specter.SecurityScheme` (core types) carry JSON tags.
yaml.v3 decodes into them by matching the lowercased field name when no `yaml:`
tag is present, which does not match camelCase JSON keys like `bearerFormat`.
To keep both formats identical, the fields the config actually exposes are
covered: `fileConfig` gets `yaml:` tags, and the nested `Server`/`SecurityScheme`
keys used in config (`url`, `description`, `type`, `scheme`, `bearerFormat`,
`name`, `in`) are confirmed to round-trip — if any nested key does not match,
that is called out in implementation and given a `yaml:` tag on the core type
(guarded so it does not change JSON output).

## Data flow

`-dir` / `-config` → resolve path (explicit or first-existing default) → read
bytes → `decodeConfig` by extension → `fileConfig` → existing field-copy +
flag-precedence logic → `cfg`.

## Error handling

- Unreadable explicit file: existing behaviour (error).
- Malformed YAML/JSON: error naming the file (as today, via `%s: %w`), now for
  either format.
- No config and no `-config`: not an error.

## Testing (TDD)

- `specter.yaml` auto-loaded from `-dir` applies its settings (mirror
  `TestConfigFileSetsAdapter`/`TestConfigFileFillsTheDocument` with a YAML body).
- `-config x.yaml` applies YAML; `-config x.yml` too.
- A `specter.json` present alongside a `specter.yaml` uses the JSON (documented
  precedence); confirming YAML does not override an existing JSON.
- Malformed YAML exits non-zero and names the file.
- Existing JSON tests stay green (regression: JSON path untouched).
- Nested `servers`/`security` decode correctly from YAML (a server URL and a
  security scheme survive the round-trip).
- Full `go test ./...` stays green.

## Non-goals

- Manual route supplements or any new config field.
- Changing JSON behaviour, precedence of flags over file, or the default
  filename set beyond adding the two YAML names.
- Writing config (Specter reads config, never writes it).
