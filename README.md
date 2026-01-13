# LLM Tools for Go

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppipada/llmtools-go)](https://goreportcard.com/report/github.com/ppipada/llmtools-go)
[![lint](https://github.com/ppipada/llmtools-go/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/ppipada/llmtools-go/actions/workflows/lint.yml)

LLM Tool implementations for Golang

- [Features at a glance](#features-at-a-glance)
- [Installation](#installation)
- [Quickstart](#quickstart)
- [Examples](#examples)
- [Notes](#notes)
- [Development](#development)

## Features at a glance

## Installation

```bash
# Go 1.25+
go get github.com/ppipada/llmtools-go
```

## Quickstart

## Examples

## Notes

## Development

- Formatting follows `gofumpt` and `golines` via `golangci-lint`, which is also used for linting. All rules are in [.golangci.yml](.golangci.yml).
- Useful scripts are defined in `taskfile.yml`; requires [Task](https://taskfile.dev/).
- Bug reports and PRs are welcome:
  - Keep the public API (`package llmtools` and `spec`) small and intentional.
  - Avoid leaking provider‑specific types through the public surface; put them under `internal/`.
  - Please run tests and linters before sending a PR.
