# Contributing to kubenow

Thank you for your interest in contributing to kubenow! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Testing](#testing)
- [Submitting Changes](#submitting-changes)
- [Code Style](#code-style)
- [Project Structure](#project-structure)

---

## Code of Conduct

Be respectful, constructive, and professional in all interactions.

---

## Getting Started

### Prerequisites

- **Go** ≥ 1.21
- **kubectl** configured with access to a Kubernetes cluster (for testing)
- **make** (for build automation)
- **git**
- **golangci-lint** (optional, for local linting)

### Fork and Clone

```bash
# Fork the repository on GitHub, then:
git clone https://github.com/YOUR_USERNAME/kubenow.git
cd kubenow

# Add upstream remote
git remote add upstream https://github.com/ppiankov/kubenow.git
```

---

## Development Setup

### 1. Install Dependencies

```bash
make deps
```

This downloads all Go modules and tidies dependencies.

### 2. Build

```bash
make build
```

Binary will be created at `bin/kubenow`.

### 3. Run Tests

```bash
make test
```

### 4. Run Linter (Optional)

```bash
make lint
```

**Note:** If golangci-lint is not installed:
```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
  sh -s -- -b $(go env GOPATH)/bin
```

---

## Making Changes

### Branching Strategy

Create a feature branch from `main`:

```bash
git checkout -b feature/my-new-feature
# or
git checkout -b fix/bug-description
```

**Branch naming conventions:**
- `feature/*` - New features
- `fix/*` - Bug fixes
- `docs/*` - Documentation updates
- `refactor/*` - Code refactoring
- `test/*` - Test improvements

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `refactor`: Code refactoring
- `test`: Test additions or fixes
- `chore`: Build, CI, or tooling changes
- `perf`: Performance improvements

**Examples:**
```
feat(analyze): add requests-skew command

Implements resource over-provisioning analysis using Prometheus metrics.
Calculates skew ratio and impact scores for workloads.

Closes #42
```

```
fix(metrics): handle empty prometheus response

Fixes panic when Prometheus returns no data for a query.

Fixes #56
```

---

## Testing

### Unit Tests

```bash
# Run all tests
make test

# Run specific package tests
go test ./internal/analyzer -v

# Run with coverage
make test-coverage
```

### Integration Tests

```bash
# Run integration tests (requires Kubernetes cluster)
go test ./test/integration -v
```

### Test Guidelines

- Write tests for new features
- Maintain or improve code coverage
- Use table-driven tests where appropriate
- Mock external dependencies (Prometheus, Kubernetes API)

**Example test structure:**
```go
func TestMyFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"case1", "input1", "output1"},
        {"case2", "input2", "output2"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MyFeature(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

---

## Submitting Changes

### Before Submitting

1. **Run all checks:**
   ```bash
   make check
   ```

2. **Ensure tests pass:**
   ```bash
   make test
   ```

3. **Update documentation** if needed

4. **Add or update tests** for your changes

### Create Pull Request

1. **Push your branch:**
   ```bash
   git push origin feature/my-new-feature
   ```

2. **Open a Pull Request** on GitHub

3. **Fill in the PR template:**
   - Description of changes
   - Related issue numbers
   - Testing performed
   - Screenshots (if UI changes)

### PR Review Process

- **CI checks** must pass (tests, lint, build)
- At least **one approving review** required
- Maintainers may request changes
- Be responsive to feedback

### After Approval

- **Squash commits** if requested
- Maintainer will merge when ready

---

## Code Style

### Go Style Guide

Follow standard Go conventions:
- Use `gofmt` for formatting
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use meaningful variable names
- Add comments for exported functions

### Linter Configuration

We use `golangci-lint` with configuration in `.golangci.yml`.

**Key rules:**
- Max line length: 140 characters
- Max cyclomatic complexity: 15
- Required error checking
- No unused code

### Running Formatters

```bash
# Format code
make fmt

# Run vet
make vet
```

---

## Project Structure

```
kubenow/
├── cmd/
│   └── kubenow/          # Main entry point
├── internal/
│   ├── analyzer/         # Analysis logic (requests-skew, node-footprint)
│   ├── cli/              # CLI commands (Cobra)
│   ├── export/           # Export formats (JSON, Markdown, HTML)
│   ├── llm/              # LLM client
│   ├── metrics/          # Prometheus integration
│   ├── models/           # Data models
│   ├── prompt/           # LLM prompt templates
│   ├── result/           # Result rendering
│   ├── snapshot/         # Kubernetes snapshot collection
│   ├── util/             # Utilities
│   └── watch/            # Watch mode
├── test/
│   ├── fixtures/         # Test data
│   └── integration/      # Integration tests
├── docs/                 # Documentation
├── .github/workflows/    # CI/CD
├── Makefile              # Build automation
├── .golangci.yml         # Linter config
└── go.mod                # Go modules
```

### Adding a New Command

1. **Create command file:**
   ```bash
   # internal/cli/mycommand.go
   ```

2. **Implement command:**
   ```go
   package cli

   import "github.com/spf13/cobra"

   var myCmd = &cobra.Command{
       Use:   "mycommand",
       Short: "My command description",
       RunE:  runMyCommand,
   }

   func init() {
       rootCmd.AddCommand(myCmd)
       // Add flags
   }

   func runMyCommand(cmd *cobra.Command, args []string) error {
       // Implementation
       return nil
   }
   ```

3. **Add tests:**
   ```bash
   # internal/cli/mycommand_test.go
   ```

4. **Update documentation**

---

## Common Tasks

### Adding a New Analyzer

1. Create analyzer in `internal/analyzer/`
2. Implement analyzer logic
3. Create CLI command in `internal/cli/`
4. Add unit tests
5. Update README with examples

### Adding Prometheus Queries

1. Update `internal/metrics/query.go`
2. Add query builder method
3. Test with mock Prometheus
4. Document query purpose

### Modifying LLM Prompts

1. Update `internal/prompt/templates.go`
2. Test with local LLM
3. Verify JSON response parsing
4. Update result rendering if schema changes

---

## Getting Help

- **GitHub Issues**: [Report bugs or request features](https://github.com/ppiankov/kubenow/issues)
- **Discussions**: [Ask questions](https://github.com/ppiankov/kubenow/discussions)
- **Documentation**: [Read the docs](https://github.com/ppiankov/kubenow/tree/main/docs)

---

## Recognition

Contributors will be recognized in:
- Release notes
- GitHub contributors page
- Changelog

---

Thank you for contributing to kubenow! 🙏
