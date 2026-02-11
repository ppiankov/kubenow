# Migration Guide: kubenow v0.1.x → v0.2+

This guide helps you migrate from kubenow v0.1.x to v0.2+.

---

## Overview

**v0.2 introduced a new Cobra-based CLI structure** with subcommands instead of the `--mode` flag. This breaking change provides better organization, improved help text, and consistency with the spectre tools family.

**Migration Difficulty:** Low
**Estimated Time:** 5-10 minutes to update scripts

---

## Breaking Changes

### 1. Command Syntax

The `--mode` flag has been removed. Each mode is now a dedicated subcommand.

#### Incident Mode

**Before (v0.1.x):**
```bash
kubenow --mode incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

#### Pod Mode

**Before (v0.1.x):**
```bash
kubenow --mode pod --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow pod --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

#### Teamlead Mode

**Before (v0.1.x):**
```bash
kubenow --mode teamlead --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow teamlead --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

#### Compliance Mode

**Before (v0.1.x):**
```bash
kubenow --mode compliance --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow compliance --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

#### Chaos Mode

**Before (v0.1.x):**
```bash
kubenow --mode chaos --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow chaos --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

#### Default Mode

**Before (v0.1.x):**
```bash
kubenow --mode default --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
# or just
kubenow --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow default --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
```

---

### 2. Global Flags Position

**Global flags** (like `--kubeconfig`, `--namespace`, `--verbose`) must now appear **before** the subcommand.

**Before (v0.1.x):**
```bash
kubenow --mode incident --namespace production --llm-endpoint ...
```

**After (v0.2+):**
```bash
kubenow --namespace production incident --llm-endpoint ...
```

**Examples:**

```bash
# Correct
kubenow --kubeconfig ~/.kube/config incident --llm-endpoint ...
kubenow --namespace prod pod --llm-endpoint ...
kubenow --verbose teamlead --llm-endpoint ...

# Incorrect
kubenow incident --kubeconfig ~/.kube/config --llm-endpoint ...
kubenow pod --namespace prod --llm-endpoint ...
```

---

### 3. Exit Codes

v0.2+ uses standardized exit codes across all commands.

| Exit Code | Meaning | v0.1.x Behavior | v0.2+ Behavior |
|-----------|---------|---------------|---------------|
| 0 | Success | Same | Same |
| 1 | Policy failure | Used for all errors | Reserved (not used yet) |
| 2 | Invalid input | Used for all errors | Validation errors only |
| 3 | Runtime error | Used for all errors | API/network failures only |

**Migration Action:** If your scripts check exit codes, update them to handle codes 2 and 3 separately.

---

### 4. Help Command

**Before (v0.1.x):**
```bash
kubenow --help
```

**After (v0.2+):**
```bash
kubenow --help                    # Show all commands
kubenow incident --help           # Show incident command help
kubenow analyze --help            # Show analyze subcommands
kubenow analyze requests-skew --help  # Show requests-skew help
```

---

## New Features in v0.2+

### Deterministic Analysis Commands

Two new command groups for cost optimization without LLMs:

#### 1. requests-skew

**Find over-provisioned resources:**

```bash
kubenow analyze requests-skew --prometheus-url http://prometheus:9090
```

**Options:**
- `--window 30d` - Time window (default: 30 days)
- `--top 10` - Top N results (default: 10)
- `--namespace-regex ".*"` - Namespace filter
- `--output table|json` - Output format
- `--export-file path` - Save to file

#### 2. node-footprint

**Simulate alternative node topologies:**

```bash
kubenow analyze node-footprint --prometheus-url http://prometheus:9090
```

**Options:**
- `--window 30d` - Time window
- `--percentile p95` - Usage percentile (p50/p95/p99)
- `--node-types "c5.xlarge,c5.2xlarge"` - Custom node types
- `--output table|json` - Output format

---

## Migration Checklist

### For Scripts and Automation

- [ ] Replace `--mode` flag with subcommand
- [ ] Move global flags before subcommand
- [ ] Update exit code handling (if checking exit codes)
- [ ] Test all scripts with v0.2+

### For CI/CD Pipelines

- [ ] Update kubenow installation (download v0.2+ binary)
- [ ] Update command syntax in pipeline scripts
- [ ] Verify exit code handling
- [ ] Test pipeline end-to-end

### For Kubernetes Jobs/CronJobs

- [ ] Update container image to v0.2+
- [ ] Update command in pod spec
- [ ] Test job execution

---

## Example Migrations

### Example 1: Simple Script

**Before (v0.1.x):**
```bash
#!/bin/bash
kubenow \
  --mode incident \
  --llm-endpoint http://llm-service:8080/v1 \
  --model mixtral:8x22b \
  --output /reports/incident-$(date +%Y%m%d).json

if [ $? -ne 0 ]; then
  echo "Kubenow failed"
  exit 1
fi
```

**After (v0.2+):**
```bash
#!/bin/bash
kubenow incident \
  --llm-endpoint http://llm-service:8080/v1 \
  --model mixtral:8x22b \
  --output /reports/incident-$(date +%Y%m%d).json

EXIT_CODE=$?
if [ $EXIT_CODE -eq 2 ]; then
  echo "Invalid input/configuration"
  exit 1
elif [ $EXIT_CODE -eq 3 ]; then
  echo "Runtime error (API/network)"
  exit 1
fi
```

---

### Example 2: Kubernetes CronJob

**Before (v0.1.x):**
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: kubenow-compliance
spec:
  schedule: "0 9 * * 1"  # Every Monday at 9am
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: kubenow
            image: ppiankov/kubenow:1.2.0
            command:
            - /kubenow
            - --mode
            - compliance
            - --llm-endpoint
            - http://llm-service:8080/v1
            - --model
            - mixtral:8x22b
            - --output
            - /reports/compliance-report.json
```

**After (v0.2+):**
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: kubenow-compliance
spec:
  schedule: "0 9 * * 1"  # Every Monday at 9am
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: kubenow
            image: ppiankov/kubenow:2.0.0
            command:
            - /kubenow
            - compliance          # Changed: subcommand instead of --mode
            - --llm-endpoint
            - http://llm-service:8080/v1
            - --model
            - mixtral:8x22b
            - --output
            - /reports/compliance-report.json
```

---

### Example 3: Watch Mode

**Before (v0.1.x):**
```bash
kubenow \
  --mode incident \
  --watch-interval 1m \
  --watch-alert-new-only \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow incident \
  --watch-interval 1m \
  --watch-alert-new-only \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

---

### Example 4: With Filters

**Before (v0.1.x):**
```bash
kubenow \
  --mode pod \
  --namespace production \
  --include-pods "payment-*,checkout-*" \
  --exclude-keywords "debug,trace" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

**After (v0.2+):**
```bash
kubenow --namespace production pod \
  --include-pods "payment-*,checkout-*" \
  --exclude-keywords "debug,trace" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

**Note:** `--namespace` moved before subcommand.

---

## Testing Your Migration

### 1. Verify Installation

```bash
kubenow version
# Should show: kubenow version 2.0.0
```

### 2. Test Commands

```bash
# Test help
kubenow --help

# Test a simple command
kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

# Test with filters
kubenow --namespace default pod --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

# Test new analyze commands
kubenow analyze requests-skew --prometheus-url http://localhost:9090
```

### 3. Verify Exit Codes

```bash
# Valid command should exit 0
kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
echo $?  # Should be 0

# Invalid flag should exit 2
kubenow incident --invalid-flag
echo $?  # Should be 2

# Missing endpoint should exit 2
kubenow incident --model mixtral:8x22b
echo $?  # Should be 2
```

---

## Rollback Plan

If you need to roll back to v0.1.x:

### Option 1: Keep Both Versions

```bash
# Rename v0.2+ binary
mv /usr/local/bin/kubenow /usr/local/bin/kubenow2

# Install v0.1.x as kubenow
curl -LO https://github.com/ppiankov/kubenow/releases/download/v1.2.0/kubenow_1.2.0_linux_amd64.tar.gz
tar -xzf kubenow_1.2.0_linux_amd64.tar.gz
sudo mv kubenow /usr/local/bin/

# Use v0.1.x: kubenow
# Use v0.2+: kubenow2
```

### Option 2: Downgrade

```bash
# Remove v0.2+
rm /usr/local/bin/kubenow

# Install v0.1.x
curl -LO https://github.com/ppiankov/kubenow/releases/download/v1.2.0/kubenow_1.2.0_linux_amd64.tar.gz
tar -xzf kubenow_1.2.0_linux_amd64.tar.gz
sudo mv kubenow /usr/local/bin/
```

---

## Getting Help

**Issues with migration?**

1. Check [GitHub Issues](https://github.com/ppiankov/kubenow/issues)
2. Review [examples in README](https://github.com/ppiankov/kubenow#usage)
3. Open a new issue with `migration` label

---

## FAQ

**Q: Can I use both v0.1.x and v0.2+ side by side?**
A: Yes, install them as separate binaries (`kubenow` and `kubenow2`).

**Q: Will v0.1.x continue to receive updates?**
A: No, v0.1.x is end-of-life. All future development is on v0.2+.

**Q: Do I need to migrate immediately?**
A: We recommend migrating within 30 days. v0.1.x will not receive security updates.

**Q: Are there any feature differences besides CLI syntax?**
A: v0.2+ adds new `analyze` commands. All v0.1.x LLM features remain unchanged.

**Q: Will my old scripts break?**
A: Yes, if you don't update them. The `--mode` flag no longer exists in v0.2+.

**Q: Can I automate the migration?**
A: Yes, with sed/awk:
```bash
# Example: Update --mode incident to incident subcommand
sed -i 's/kubenow --mode incident/kubenow incident/g' my-script.sh
```

---

**Migration complete? Welcome to kubenow v0.2+!** 🎉
