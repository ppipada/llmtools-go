# LLM Tools for Go

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/flexigpt/llmtools-go)](https://goreportcard.com/report/github.com/flexigpt/llmtools-go)
[![lint](https://github.com/flexigpt/llmtools-go/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/flexigpt/llmtools-go/actions/workflows/lint.yml)
[![test](https://github.com/flexigpt/llmtools-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/flexigpt/llmtools-go/actions/workflows/test.yml)

Go-native and cross-platform "tool" implementations for common local tasks, plus a small registry that makes them easy to expose to LLM tool-calling systems.

## Table of contents

- [Table of contents](#table-of-contents)
- [Features at a glance](#features-at-a-glance)
  - [File system tools](#file-system-tools)
  - [Execute tools](#execute-tools)
  - [Text processing tools](#text-processing-tools)
  - [Image tools](#image-tools)
- [Package overview](#package-overview)
- [Registry](#registry)
- [Tool outputs](#tool-outputs)
- [Sandboxing \& path policy](#sandboxing--path-policy)
- [Examples](#examples)
- [Shell / exec tool notes](#shell--exec-tool-notes)
- [Development](#development)
- [License](#license)

## Features at a glance

- Sandboxed execution for all tools under a enforced allowed roots and work directory.
- Symlinks are rejected throughout for safety.
- All implementations are cross-platform supporting lin/win/mac systems.
- Normalized tool spec to support text/image/binary output formats.
- Fully modular code for any customizations needed.

### File system tools

- Grouped under: `fstool`.

- `readfile`:
  - `encoding=text`: reads UTF-8 text only (rejects non-text), with PDF text extraction support when the file is a PDF.
  - `encoding=binary`: returns base64, emitting `image` outputs for `image/*` MIME types and `file` outputs otherwise.
  - Safety: size caps and symlink-traversal hardening.

- `writefile`:
  - `encoding=text`: write UTF-8 content
  - `encoding=binary`: write base64-decoded bytes
  - Options: `overwrite`, `createParents` (bounded), atomic writes, size caps, symlink hardening.

- `deletefile`:
  - “Safe delete” by moving to trash.
  - `trashDir=auto` tries system trash when possible; falls back to a local `.trash` directory.
  - Uses unique naming, best-effort cross-device handling, and avoids destructive removal when possible.

- `searchfiles`: Recursively search file paths and UTF-8 text content using RE2 regex.

- `listdirectory`: List entries under a directory, optionally filtered by glob.

- `statpath`: Inspect a path (exists, size, timestamps, directory flag).
- `mimeforpath`: Best-effort MIME type detection (extension + sniffing).
- `mimeforextension`: MIME lookup for an extension.

### Execute tools

- Grouped under: `exectool`

- `shellcommand`:
  - Execute one or more commands via a selected shell (`auto`, `sh`, `bash`, `pwsh`, `powershell`, `cmd`, etc).
  - Enforces timeouts/output caps/command caps.
  - Supports session-like persistence for workdir and environment across calls (note: _not_ a persistent shell process).

- `runscript`:
  - Run an existing script from disk with arguments and environment overrides.
  - Extension-based interpreter selection via `RunScriptPolicy` (host-configurable).

### Text processing tools

- Grouped under: `texttool`

- `readtextrange`: Read lines, optionally constrained by unique start/end marker blocks.
- `findtext`: Find matches with context (modes: substring, regex (RE2/Go), exact line-block).
- `inserttextlines`: Insert lines at start/end or relative to a uniquely matched anchor block.
- `replacetextlines`: Replace exact line blocks; can disambiguate with immediate adjacent `beforeLines`/`afterLines`.
- `deletetextlines`: Delete exact line blocks; can disambiguate with immediate adjacent `beforeLines`/`afterLines`.

### Image tools

- Grouped under: `imagetool`

- `readimage`: Read intrinsic metadata (width/height/format/MIME), optionally include base64 content.

## Package overview

- `llmtools`: Registry + tool registration helpers.
- `spec`: Tool manifests + output union types.
- `fstool`: Filesystem tools.
- `exectool`: Shell command execution and script execution.
- `texttool`: Safe, deterministic line-based text editing tools.
- `imagetool`: Image tools.

## Registry

The registry provides:

- tool registration + lookup by `spec.FuncID`
- stable manifest ordering (`Tools()` sorted by slug + funcID)
- per-registry default call timeout via `WithDefaultCallTimeout`
- per-call timeout override via `llmtools.WithCallTimeout(...)`
- panic-to-error recovery around tool execution

## Tool outputs

- `Registry.Call` returns `[]spec.ToolStoreOutputUnion`.

- The call wrapper can modify the union to support two common patterns:
  - Structured JSON-as-text (most tools)
    - Most tools are registered via `RegisterTypedAsTextTool`, which wraps the tool’s Go output as JSON and returns it as a single `text` output item.

  - Typed content outputs
    - `text` output for UTF-8 text / extracted PDF text
    - `image` output for images when `encoding=binary`
    - `file` output for all other binaries when `encoding=binary`
    - E.g.: `readfile`: This output makes `readfile` suitable for LLM systems that support multi-modal/file outputs.

## Sandboxing & path policy

All `Tools` are are _instance-owned_ tools. Hosts can configure:

- `workBaseDir`: base directory for resolving relative paths
- `allowedRoots`: optional allowlist roots; when set, all resolved paths must stay within these roots

This is the recommended way to run the tools safely inside a sandbox (for example, inside a temp workspace or per-user directory).

## Examples

All examples are provided as end-to-end integration tests that:

- start from a registry
- register tools (sandboxed to a temp directory)
- execute realistic sequences: read/modify loops, text edits, shell sessions, script execution, binary/image workflows

Examples:

- Text read/modify loop (find/replace/insert/delete + verification): [`text test`](internal/integration/text_test.go)
- Filesystem + MIME + safe delete (trash) + binary/image flows: [`fs + image test`](internal/integration/fs_image_test.go)
- Shell sessions + environment persistence + runscript: [`exec test`](internal/integration/exec_test.go)

## Shell / exec tool notes

- OS support
  - Cross-platform shell selection: `auto` chooses a safe default per OS.
  - Windows prefers `pwsh`, then Windows PowerShell, then `cmd`.

- Timeouts
  - The registry may enforce a call timeout (`WithDefaultCallTimeout`).
  - `shellcommand` and `runscript` also enforce execution policy timeouts.
  - Ensure the registry timeout is >= tool execution timeout (or set registry timeout to 0) to avoid premature cancellation.

- Safety knobs
  - `ExecutionPolicy` caps total commands, command length, output bytes, and timeout.
  - “Hard blocked” commands are always blocked.
  - Heuristic checks (fork-bomb/backgrounding patterns) can be toggled via `AllowDangerous`.

- RunScriptPolicy
  - Interpreter selection is extension-based and host-configurable.
  - Hosts can tighten allowed extensions and interpreter mappings.

## Development

- Formatting follows `gofumpt` and `golines` via `golangci-lint`. Rules are in [.golangci.yml](.golangci.yml).
- Useful scripts are defined in `taskfile.yml`; requires [Task](https://taskfile.dev/).
- Bug reports and PRs are welcome:
  - Keep the public API (`package llmtools` and `spec`) small and intentional.
  - Avoid leaking provider‑specific types through the public surface; put them under `internal/`.
  - Please run tests and linters before sending a PR.

## License

Copyright (c) 2026 - Present - Pankaj Pipada

All source code in this repository, unless otherwise noted, is licensed under the MIT License.
See [LICENSE](./LICENSE) for details.
