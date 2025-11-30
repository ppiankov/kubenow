// CLI entrypoint.

package main

import (
    "context"
    "flag"
    "fmt"
    "os"

    "kubenow/internal/llm"
    "kubenow/internal/prompt"
    "kubenow/internal/snapshot"
    "kubenow/internal/util"
)

func main() {
    kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig (defaults to KUBECONFIG or ~/.kube/config)")
    endpoint   := flag.String("llm-endpoint", "", "OpenAI-compatible API URL (e.g. http://host:11434/v1)")
    model      := flag.String("model", "mixtral:8x22b", "model name (e.g. mixtral:8x22b, gpt-4.1-mini)")
    mode       := flag.String("mode", "default", "mode: default|pod|incident|teamlead|compliance|chaos")
    namespace  := flag.String("namespace", "", "namespace to inspect (empty = all non-system)")
    maxPods    := flag.Int("max-pods", 10, "max problem pods to include in snapshot")
    logLines   := flag.Int64("log-lines", 50, "log lines per container")
    apiKey     := flag.String("api-key", os.Getenv("KUBENOW_API_KEY"), "LLM API key (if required by endpoint)")

    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, `kubenow - Kubernetes now-view with LLM brain
"11434 is enough."

Usage:
  kubenow [flags]

Core flags:
`)
        flag.PrintDefaults()

        fmt.Fprintf(os.Stderr, `

Modes:
  default    - cluster summary, top issues, human-readable
  pod        - deep dive on individual problematic pods
  incident   - outage mode: short, ranked, fix-first commands
  teamlead   - teamlead view: risk, ownership hints, what to escalate
  compliance - policy-ish view: resources, images, basic hygiene
  chaos      - suggests chaos experiments / failure drills based on weaknesses

Examples:
  kubenow --llm-endpoint http://192.168.1.144:11434/v1 --model mixtral:8x22b --mode incident
  kubenow --llm-endpoint https://api.openai.com/v1 --model gpt-4.1-mini --api-key $KUBENOW_API_KEY --mode teamlead
`)
    }

    flag.Parse()

    if *endpoint == "" {
        fmt.Fprintln(os.Stderr, "Error: --llm-endpoint is required")
        flag.Usage()
        os.Exit(1)
    }

    if err := prompt.ValidateMode(*mode); err != nil {
        fmt.Fprintln(os.Stderr, err)
        flag.Usage()
        os.Exit(1)
    }

    ctx := context.Background()

    fmt.Fprintln(os.Stderr, "[kubenow] Building Kubernetes client...")
    client, err := util.BuildClient(*kubeconfig)
    if err != nil {
        fmt.Fprintln(os.Stderr, "kubeconfig error:", err)
        os.Exit(1)
    }

    fmt.Fprintln(os.Stderr, "[kubenow] Collecting cluster snapshot...")
    snap, err := snapshot.Collect(ctx, client, *namespace, *maxPods, *logLines)
    if err != nil {
        fmt.Fprintln(os.Stderr, "snapshot error:", err)
        os.Exit(1)
    }

    ppath := prompt.PromptPath(*mode)
    if ppath == "" {
        fmt.Fprintln(os.Stderr, "unknown mode:", *mode)
        os.Exit(1)
    }

    fullPrompt, err := prompt.LoadPrompt(ppath, snap.JSON())
    if err != nil {
        fmt.Fprintln(os.Stderr, "prompt load error:", err)
        os.Exit(1)
    }

    fmt.Fprintln(os.Stderr, "[kubenow] Calling LLM endpoint:", *endpoint)
    answer, err := llm.QueryLLM(ctx, *endpoint, *apiKey, *model, fullPrompt)
    if err != nil {
        fmt.Fprintln(os.Stderr, "llm error:", err)
        os.Exit(1)
    }

    // Final output to stdout so it can be piped
    fmt.Println(answer)
}
