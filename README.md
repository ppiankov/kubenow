![kubenow logo](https://raw.githubusercontent.com/ppiankov/kubenow/main/docs/img/logo.png)

# 🧯 kubenow — Kubernetes Incident Triage on Demand

“11434 is enough.”

## kubenow is a single Go binary that takes a live Kubernetes cluster snapshot and feeds it into an LLM (local or cloud) to generate:
	•	🔥 incident triage (ranked, actionable, command-ready)
	•	🛠 pod-level debugging
	•	📊 cluster health summaries
	•	👩‍💼 teamlead / ownership recommendations
	•	🧹 compliance / hygiene reviews
	•	🧨 chaos engineering experiment suggestions

## It works with any OpenAI-compatible API, including:
	•	🦙 Ollama (Mixtral, Llama, Qwen, etc.)
	•	☁️ OpenAI / Azure OpenAI
	•	🔧 DeepSeek / Groq / Together / OpenRouter
	•	or your own weird homemade inference server

If your laptop can run it and respond to /v1/chat/completions,
kubenow will talk to it.

# ✨ Why kubenow?

## Because when the cluster is on fire, nobody wants to run:
	•	12 commands
	•	across 5 namespaces
	•	using 4 terminals
	•	while Slack is screaming

You want:
```bash
TOP ISSUES:
1. callback/data-converter-worker — ImagePullBackOff — critical
2. payments-api — CrashLoopBackOff — high

ROOT CAUSES:
1. Private registry unreachable from nodes.
2. readinessProbe fails immediately.

FIX COMMANDS:
kubectl -n callback get events
kubectl -n callback set image deploy/data-converter-worker api=repo/worker:stable
kubectl -n prod edit deploy/payments-api
```

Short, ranked, actionable.

And yes — kubenow can also run teamlead mode, which gently hints at which team probably caused the outage.

# 🧩 Features

## 🔥 Incident Mode (--mode incident)
	•	Ranks the top problems in the cluster
	•	Gives 1–2 sentence root causes
	•	Provides actionable kubectl / YAML patches
	•	Zero fluff, zero theory

## 🧪 Pod Mode (--mode pod)

Deep dive into broken pods:
	•	container states
	•	events
	•	restarts
	•	image pulls
	•	OOMs
	•	last logs

## 📊 Default Mode

High-level cluster summary with readable health insights.

## 👩‍💼 Teamlead Mode (--mode teamlead)

Manager-friendly report:
	•	risk
	•	blast radius
	•	ownership hints
	•	escalation guidance

## 📏 Compliance Mode (--mode compliance)

Finds policy / hygiene issues:
	•	missing resource limits
	•	:latest tags
	•	namespace misuse
	•	registry hygiene
	•	bad env patterns

## 🧨 Chaos Mode

Suggests targeted chaos experiments based on real weaknesses:
	•	node drain
	•	registry outage simulation
	•	disruption tests
	•	restart storms

⸻

# 📦 Installation

Build from source

Requires Go ≥ 1.25.4

```bash
git clone https://github.com/ppiankov/kubenow
cd kubenow
go build ./cmd/kubenow
```

(Optional) Move to PATH

```bash
sudo mv kubenow /usr/local/bin/
```

Helps DevOps engineers identify pods with incorrectly configured
resource limits/requests, reducing cluster waste and improving stability.

# 🚀 Usage

You only need:
	•	a kubeconfig
	•	an LLM endpoint
	•	a model name

Example (local Ollama)
```bash
./kubenow \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b \
  --mode incident
```
Example (OpenAI)

```bash
export KUBENOW_API_KEY="sk-yourkey"

./kubenow \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini \
  --mode teamlead
```

Example (one specific namespace)

```bash
./kubenow \
  --namespace prod \
  --mode pod \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```


# 🧠 Recommended Models
| Mode | Best Local | Best Cloud |notes |
|-------|-----|-----------|-----------|
| incident | mixtral:8x22b | GPT-4.1 Mini | concise, obedient|
| pod | llama3:70b (if patient) | GPT-4.1 | detail friendly |
| teamlead | mixtral:8x22b | GPT-4.1 Mini | leadership tone |
| compliance | mixtral or Qwen |GPT-4.1 Mini | structured |
| chaos | mixtral |GPT-4.1 Mini | creative but grounded|

Quote of the project:
“11434 is enough.”

# 🔧 Command-Line Flags

```bash
--kubeconfig <path>     Path to kubeconfig (optional)
--namespace <ns>        Only analyze this namespace
--mode <type>           default|pod|incident|teamlead|compliance|chaos
--llm-endpoint <url>    OpenAI-compatible URL
--model <name>          Model name (mixtral:8x22b, gpt-4.1-mini, etc.)
--api-key <key>         LLM API key (optional if local)
--max-pods <num>        Max problem pods to include (default: 10)
--log-lines <num>       Logs per container (default: 50)
```

# 🧱 Architecture

## Scenario 1: Silent LLM RAM spike

```bash
cmd/kubenow/
internal/
  snapshot/   ← collects K8s data, applies issueType classification
  prompt/     ← loads prompt templates by mode
  llm/        ← calls OpenAI-compatible APIs
  util/       ← kube client builder
prompts/
  default.txt
  pod.txt
  incident.txt
  teamlead.txt
  compliance.txt
  chaos.txt
```
Snapshot contains:
	•	node conditions
	•	problem pods with:
	•	reason
	•	restart count
	•	container states
	•	resource requests/limits
	•	image names
	•	last logs
	•	pod events
	•	issueType (ImagePullError | CrashLoop | OOMKilled | PendingScheduling | etc.)

# 📄 License
MIT

# 🐉 Disclaimer

## This tool can:
	•	shame your engineers
	•	uncover your terrible cluster hygiene
	•	predict who broke production
	•	and suggest chaos tests strong enough to get you fired

Use responsibly.

## ✨ Keywords
	•	kubernetes incident response LLM
	•	kubernetes triage cli
	•	ollama kubernetes assistant
	•	k8s troubleshooting
	•	kubectl alternative
	•	k8s observability
	•	chaos engineering

---

